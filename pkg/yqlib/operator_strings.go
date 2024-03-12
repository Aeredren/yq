package yqlib

import (
	"container/list"
	"fmt"
	"regexp"
	"strings"
)

var StringInterpolationEnabled = true

type changeCasePrefs struct {
	ToUpperCase bool
}

func encodeToYamlString(node *CandidateNode) (string, error) {
	encoderPrefs := encoderPreferences{
		format: YamlFormat,
		indent: ConfiguredYamlPreferences.Indent,
	}
	result, err := encodeToString(node, encoderPrefs)

	if err != nil {
		return "", err
	}
	return chomper.ReplaceAllString(result, ""), nil
}

func evaluate(d *dataTreeNavigator, context Context, expStr string) (string, error) {
	exp, err := ExpressionParser.ParseExpression(expStr)
	if err != nil {
		return "", err
	}
	result, err := d.GetMatchingNodes(context, exp)
	if err != nil {
		return "", err
	}
	if result.MatchingNodes.Len() == 0 {
		return "", nil
	}
	node := result.MatchingNodes.Front().Value.(*CandidateNode)
	if node.Kind != ScalarNode {
		return encodeToYamlString(node)
	}
	return node.Value, nil
}

func interpolate(d *dataTreeNavigator, context Context, str string) (string, error) {
	var sb strings.Builder
	var expSb strings.Builder
	inExpression := false
	nestedBracketsCounter := 0
	runes := []rune(str)
	for i := 0; i < len(runes); i++ {
		char := runes[i]
		if !inExpression {
			if char == '\\' && i != len(runes)-1 && runes[i+1] == '(' {
				inExpression = true
				i = i + 1 // skip over the next open bracket
				continue
			}
			sb.WriteRune(char)
		} else { // we are in an expression
			if char == ')' && nestedBracketsCounter == 0 {
				// finished the expression!
				log.Debugf("Expression is :%v", expSb.String())
				value, err := evaluate(d, context, expSb.String())
				if err != nil {
					return "", err
				}

				inExpression = false
				expSb = strings.Builder{} // reset this

				sb.WriteString(value)
				continue
			} else if char == '(' {
				nestedBracketsCounter++
			} else if char == '\\' && i != len(runes)-1 && runes[i+1] == ')' {
				// close brackets is escaped, skip over it
				expSb.WriteRune(char)
				expSb.WriteRune(runes[i+1])
				i = i + 1
				continue
			} else if char == ')' {
				nestedBracketsCounter--
			}
			expSb.WriteRune(char)
		}

	}
	if inExpression {
		return "", fmt.Errorf("unclosed interpolation string \\(")
	}
	return sb.String(), nil
}

func stringInterpolationOperator(d *dataTreeNavigator, context Context, expressionNode *ExpressionNode) (Context, error) {
	if !StringInterpolationEnabled {
		return context.SingleChildContext(
			CreateScalarNode(expressionNode.Operation.StringValue, expressionNode.Operation.StringValue),
		), nil
	}
	if context.MatchingNodes.Len() == 0 {
		value, err := interpolate(d, context, expressionNode.Operation.StringValue)
		if err != nil {
			return Context{}, err
		}
		node := CreateScalarNode(value, value)
		return context.SingleChildContext(node), nil
	}

	var results = list.New()

	for el := context.MatchingNodes.Front(); el != nil; el = el.Next() {
		candidate := el.Value.(*CandidateNode)
		value, err := interpolate(d, context.SingleChildContext(candidate), expressionNode.Operation.StringValue)
		if err != nil {
			return Context{}, err
		}
		node := CreateScalarNode(value, value)
		results.PushBack(node)
	}

	return context.ChildContext(results), nil
}

func trimSpaceOperator(_ *dataTreeNavigator, context Context, _ *ExpressionNode) (Context, error) {
	results := list.New()
	for el := context.MatchingNodes.Front(); el != nil; el = el.Next() {
		node := el.Value.(*CandidateNode)

		if node.GuessTagFromCustomType() != "!!str" {
			return Context{}, fmt.Errorf("cannot trim %v, can only operate on strings. ", node.Tag)
		}

		newStringNode := node.CreateReplacement(ScalarNode, node.Tag, strings.TrimSpace(node.Value))
		newStringNode.Style = node.Style
		results.PushBack(newStringNode)

	}

	return context.ChildContext(results), nil
}

func toStringOperator(_ *dataTreeNavigator, context Context, _ *ExpressionNode) (Context, error) {
	results := list.New()
	for el := context.MatchingNodes.Front(); el != nil; el = el.Next() {
		node := el.Value.(*CandidateNode)
		var newStringNode *CandidateNode
		if node.Tag == "!!str" {
			newStringNode = node.CreateReplacement(ScalarNode, "!!str", node.Value)
		} else if node.Kind == ScalarNode {
			newStringNode = node.CreateReplacement(ScalarNode, "!!str", node.Value)
			newStringNode.Style = DoubleQuotedStyle
		} else {
			result, err := encodeToYamlString(node)
			if err != nil {
				return Context{}, err
			}
			newStringNode = node.CreateReplacement(ScalarNode, "!!str", result)
			newStringNode.Style = DoubleQuotedStyle
		}
		newStringNode.Tag = "!!str"
		results.PushBack(newStringNode)
	}

	return context.ChildContext(results), nil
}

func changeCaseOperator(_ *dataTreeNavigator, context Context, expressionNode *ExpressionNode) (Context, error) {
	results := list.New()
	prefs := expressionNode.Operation.Preferences.(changeCasePrefs)

	for el := context.MatchingNodes.Front(); el != nil; el = el.Next() {
		node := el.Value.(*CandidateNode)

		if node.GuessTagFromCustomType() != "!!str" {
			return Context{}, fmt.Errorf("cannot change case with %v, can only operate on strings. ", node.Tag)
		}

		value := ""
		if prefs.ToUpperCase {
			value = strings.ToUpper(node.Value)
		} else {
			value = strings.ToLower(node.Value)
		}
		newStringNode := node.CreateReplacement(ScalarNode, node.Tag, value)
		newStringNode.Style = node.Style
		results.PushBack(newStringNode)

	}

	return context.ChildContext(results), nil

}

func getSubstituteParameters(d *dataTreeNavigator, block *ExpressionNode, context Context) (string, string, error) {
	regEx := ""
	replacementText := ""

	regExNodes, err := d.GetMatchingNodes(context.ReadOnlyClone(), block.LHS)
	if err != nil {
		return "", "", err
	}
	if regExNodes.MatchingNodes.Front() != nil {
		regEx = regExNodes.MatchingNodes.Front().Value.(*CandidateNode).Value
	}

	log.Debug("regEx %v", regEx)

	replacementNodes, err := d.GetMatchingNodes(context, block.RHS)
	if err != nil {
		return "", "", err
	}
	if replacementNodes.MatchingNodes.Front() != nil {
		replacementText = replacementNodes.MatchingNodes.Front().Value.(*CandidateNode).Value
	}

	return regEx, replacementText, nil
}

func substitute(original string, regex *regexp.Regexp, replacement string) (Kind, string, string) {
	replacedString := regex.ReplaceAllString(original, replacement)
	return ScalarNode, "!!str", replacedString
}

func substituteStringOperator(d *dataTreeNavigator, context Context, expressionNode *ExpressionNode) (Context, error) {
	//rhs  block operator
	//lhs of block = regex
	//rhs of block = replacement expression
	block := expressionNode.RHS

	regExStr, replacementText, err := getSubstituteParameters(d, block, context)

	if err != nil {
		return Context{}, err
	}

	regEx, err := regexp.Compile(regExStr)
	if err != nil {
		return Context{}, err
	}

	var results = list.New()

	for el := context.MatchingNodes.Front(); el != nil; el = el.Next() {
		node := el.Value.(*CandidateNode)
		if node.GuessTagFromCustomType() != "!!str" {
			return Context{}, fmt.Errorf("cannot substitute with %v, can only substitute strings. Hint: Most often you'll want to use '|=' over '=' for this operation", node.Tag)
		}

		result := node.CreateReplacement(substitute(node.Value, regEx, replacementText))
		results.PushBack(result)
	}

	return context.ChildContext(results), nil

}

func addMatch(original []*CandidateNode, match string, offset int, name string) []*CandidateNode {

	newContent := append(original,
		CreateScalarNode("string", "string"))

	if offset < 0 {
		// offset of -1 means there was no match, force a null value like jq
		newContent = append(newContent,
			CreateScalarNode(nil, "null"),
		)
	} else {
		newContent = append(newContent,
			CreateScalarNode(match, match),
		)
	}

	newContent = append(newContent,
		CreateScalarNode("offset", "offset"),
		CreateScalarNode(offset, fmt.Sprintf("%v", offset)),
		CreateScalarNode("length", "length"),
		CreateScalarNode(len(match), fmt.Sprintf("%v", len(match))))

	if name != "" {
		newContent = append(newContent,
			CreateScalarNode("name", "name"),
			CreateScalarNode(name, name),
		)
	}
	return newContent
}

type matchPreferences struct {
	Global bool
}

func getMatches(matchPrefs matchPreferences, regEx *regexp.Regexp, value string) ([][]string, [][]int) {
	var allMatches [][]string
	var allIndices [][]int

	if matchPrefs.Global {
		allMatches = regEx.FindAllStringSubmatch(value, -1)
		allIndices = regEx.FindAllStringSubmatchIndex(value, -1)
	} else {
		allMatches = [][]string{regEx.FindStringSubmatch(value)}
		allIndices = [][]int{regEx.FindStringSubmatchIndex(value)}
	}

	log.Debug("allMatches, %v", allMatches)
	return allMatches, allIndices
}

func match(matchPrefs matchPreferences, regEx *regexp.Regexp, candidate *CandidateNode, value string, results *list.List) {
	subNames := regEx.SubexpNames()
	allMatches, allIndices := getMatches(matchPrefs, regEx, value)

	// if all matches just has an empty array in it,
	// then nothing matched
	if len(allMatches) > 0 && len(allMatches[0]) == 0 {
		return
	}

	for i, matches := range allMatches {
		capturesListNode := &CandidateNode{Kind: SequenceNode}
		match, submatches := matches[0], matches[1:]
		for j, submatch := range submatches {
			captureNode := &CandidateNode{Kind: MappingNode}
			captureNode.AddChildren(addMatch(captureNode.Content, submatch, allIndices[i][2+j*2], subNames[j+1]))
			capturesListNode.AddChild(captureNode)
		}

		node := candidate.CreateReplacement(MappingNode, "!!map", "")
		node.AddChildren(addMatch(node.Content, match, allIndices[i][0], ""))
		node.AddKeyValueChild(CreateScalarNode("captures", "captures"), capturesListNode)
		results.PushBack(node)

	}

}

func capture(matchPrefs matchPreferences, regEx *regexp.Regexp, candidate *CandidateNode, value string, results *list.List) {
	subNames := regEx.SubexpNames()
	allMatches, allIndices := getMatches(matchPrefs, regEx, value)

	// if all matches just has an empty array in it,
	// then nothing matched
	if len(allMatches) > 0 && len(allMatches[0]) == 0 {
		return
	}

	for i, matches := range allMatches {
		capturesNode := candidate.CreateReplacement(MappingNode, "!!map", "")

		_, submatches := matches[0], matches[1:]
		for j, submatch := range submatches {

			keyNode := CreateScalarNode(subNames[j+1], subNames[j+1])
			var valueNode *CandidateNode

			offset := allIndices[i][2+j*2]
			// offset of -1 means there was no match, force a null value like jq
			if offset < 0 {
				valueNode = CreateScalarNode(nil, "null")
			} else {
				valueNode = CreateScalarNode(submatch, submatch)
			}
			capturesNode.AddKeyValueChild(keyNode, valueNode)
		}

		results.PushBack(capturesNode)

	}

}

func extractMatchArguments(d *dataTreeNavigator, context Context, expressionNode *ExpressionNode) (*regexp.Regexp, matchPreferences, error) {
	regExExpNode := expressionNode.RHS

	matchPrefs := matchPreferences{}

	// we got given parameters e.g. match(exp; params)
	if expressionNode.RHS.Operation.OperationType == blockOpType {
		block := expressionNode.RHS
		regExExpNode = block.LHS
		replacementNodes, err := d.GetMatchingNodes(context, block.RHS)
		if err != nil {
			return nil, matchPrefs, err
		}
		paramText := ""
		if replacementNodes.MatchingNodes.Front() != nil {
			paramText = replacementNodes.MatchingNodes.Front().Value.(*CandidateNode).Value
		}
		if strings.Contains(paramText, "g") {
			paramText = strings.ReplaceAll(paramText, "g", "")
			matchPrefs.Global = true
		}
		if strings.Contains(paramText, "i") {
			return nil, matchPrefs, fmt.Errorf(`'i' is not a valid option for match. To ignore case, use an expression like match("(?i)cat")`)
		}
		if len(paramText) > 0 {
			return nil, matchPrefs, fmt.Errorf(`Unrecognised match params '%v', please see docs at https://mikefarah.gitbook.io/yq/operators/string-operators`, paramText)
		}
	}

	regExNodes, err := d.GetMatchingNodes(context.ReadOnlyClone(), regExExpNode)
	if err != nil {
		return nil, matchPrefs, err
	}
	log.Debug(NodesToString(regExNodes.MatchingNodes))
	regExStr := ""
	if regExNodes.MatchingNodes.Front() != nil {
		regExStr = regExNodes.MatchingNodes.Front().Value.(*CandidateNode).Value
	}
	log.Debug("regEx %v", regExStr)
	regEx, err := regexp.Compile(regExStr)
	return regEx, matchPrefs, err
}

func matchOperator(d *dataTreeNavigator, context Context, expressionNode *ExpressionNode) (Context, error) {
	regEx, matchPrefs, err := extractMatchArguments(d, context, expressionNode)
	if err != nil {
		return Context{}, err
	}

	var results = list.New()

	for el := context.MatchingNodes.Front(); el != nil; el = el.Next() {
		node := el.Value.(*CandidateNode)
		if node.GuessTagFromCustomType() != "!!str" {
			return Context{}, fmt.Errorf("cannot match with %v, can only match strings. Hint: Most often you'll want to use '|=' over '=' for this operation", node.Tag)
		}

		match(matchPrefs, regEx, node, node.Value, results)
	}

	return context.ChildContext(results), nil
}

func captureOperator(d *dataTreeNavigator, context Context, expressionNode *ExpressionNode) (Context, error) {
	regEx, matchPrefs, err := extractMatchArguments(d, context, expressionNode)
	if err != nil {
		return Context{}, err
	}

	var results = list.New()

	for el := context.MatchingNodes.Front(); el != nil; el = el.Next() {
		node := el.Value.(*CandidateNode)
		if node.GuessTagFromCustomType() != "!!str" {
			return Context{}, fmt.Errorf("cannot match with %v, can only match strings. Hint: Most often you'll want to use '|=' over '=' for this operation", node.Tag)
		}
		capture(matchPrefs, regEx, node, node.Value, results)

	}

	return context.ChildContext(results), nil
}

func testOperator(d *dataTreeNavigator, context Context, expressionNode *ExpressionNode) (Context, error) {
	regEx, _, err := extractMatchArguments(d, context, expressionNode)
	if err != nil {
		return Context{}, err
	}

	var results = list.New()

	for el := context.MatchingNodes.Front(); el != nil; el = el.Next() {
		node := el.Value.(*CandidateNode)
		if node.GuessTagFromCustomType() != "!!str" {
			return Context{}, fmt.Errorf("cannot match with %v, can only match strings. Hint: Most often you'll want to use '|=' over '=' for this operation", node.Tag)
		}
		matches := regEx.FindStringSubmatch(node.Value)
		results.PushBack(createBooleanCandidate(node, len(matches) > 0))

	}

	return context.ChildContext(results), nil
}

func joinStringOperator(d *dataTreeNavigator, context Context, expressionNode *ExpressionNode) (Context, error) {
	log.Debugf("joinStringOperator")
	joinStr := ""

	rhs, err := d.GetMatchingNodes(context.ReadOnlyClone(), expressionNode.RHS)
	if err != nil {
		return Context{}, err
	}
	if rhs.MatchingNodes.Front() != nil {
		joinStr = rhs.MatchingNodes.Front().Value.(*CandidateNode).Value
	}

	var results = list.New()

	for el := context.MatchingNodes.Front(); el != nil; el = el.Next() {
		node := el.Value.(*CandidateNode)
		if node.Kind != SequenceNode {
			return Context{}, fmt.Errorf("cannot join with %v, can only join arrays of scalars", node.Tag)
		}
		result := node.CreateReplacement(join(node.Content, joinStr))
		results.PushBack(result)
	}

	return context.ChildContext(results), nil
}

func join(content []*CandidateNode, joinStr string) (Kind, string, string) {
	var stringsToJoin []string
	for _, node := range content {
		str := node.Value
		if node.Tag == "!!null" {
			str = ""
		}
		stringsToJoin = append(stringsToJoin, str)
	}

	return ScalarNode, "!!str", strings.Join(stringsToJoin, joinStr)
}

func splitStringOperator(d *dataTreeNavigator, context Context, expressionNode *ExpressionNode) (Context, error) {
	log.Debugf("splitStringOperator")
	splitStr := ""

	rhs, err := d.GetMatchingNodes(context.ReadOnlyClone(), expressionNode.RHS)
	if err != nil {
		return Context{}, err
	}
	if rhs.MatchingNodes.Front() != nil {
		splitStr = rhs.MatchingNodes.Front().Value.(*CandidateNode).Value
	}

	var results = list.New()

	for el := context.MatchingNodes.Front(); el != nil; el = el.Next() {
		node := el.Value.(*CandidateNode)
		if node.Tag == "!!null" {
			continue
		}

		if node.GuessTagFromCustomType() != "!!str" {
			return Context{}, fmt.Errorf("cannot split %v, can only split strings", node.Tag)
		}
		kind, tag, content := split(node.Value, splitStr)
		result := node.CreateReplacement(kind, tag, "")
		result.AddChildren(content)
		results.PushBack(result)
	}

	return context.ChildContext(results), nil
}

func split(value string, spltStr string) (Kind, string, []*CandidateNode) {
	var contents []*CandidateNode

	if value != "" {
		log.Debug("going to spltStr[%v]", spltStr)
		var newStrings = strings.Split(value, spltStr)
		contents = make([]*CandidateNode, len(newStrings))

		for index, str := range newStrings {
			contents[index] = &CandidateNode{Kind: ScalarNode, Tag: "!!str", Value: str}
		}
	}

	return SequenceNode, "!!seq", contents
}
