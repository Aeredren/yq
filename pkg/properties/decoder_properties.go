package properties

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/magiconair/properties"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
)

type propertiesDecoder struct {
	reader   io.Reader
	finished bool
	d        yqlib.DataTreeNavigator
}

func NewPropertiesDecoder() yqlib.Decoder {
	return &propertiesDecoder{d: yqlib.NewDataTreeNavigator(), finished: false}
}

func (dec *propertiesDecoder) Init(reader io.Reader) error {
	dec.reader = reader
	dec.finished = false
	return nil
}

func parsePropKey(key string) []interface{} {
	pathStrArray := strings.Split(key, ".")
	path := make([]interface{}, len(pathStrArray))
	for i, pathStr := range pathStrArray {
		num, err := strconv.ParseInt(pathStr, 10, 32)
		if err == nil {
			path[i] = num
		} else {
			path[i] = pathStr
		}
	}
	return path
}

func (dec *propertiesDecoder) processComment(c string) string {
	if c == "" {
		return ""
	}
	return "# " + c
}

func (dec *propertiesDecoder) applyPropertyComments(context yqlib.Context, path []interface{}, comments []string) error {
	rhsCandidateNode := &yqlib.CandidateNode{
		Tag:         "!!str",
		Value:       fmt.Sprintf("%v", path[len(path)-1]),
		HeadComment: dec.processComment(strings.Join(comments, "\n")),
		Kind:        yqlib.ScalarNode,
	}
	rhsCandidateNode.Tag = rhsCandidateNode.GuessTagFromCustomType()
	return dec.d.DeeplyAssignKey(context, path, rhsCandidateNode)
}

func (dec *propertiesDecoder) applyProperty(context yqlib.Context, properties *properties.Properties, key string) error {
	value, _ := properties.Get(key)
	path := parsePropKey(key)

	propertyComments := properties.GetComments(key)
	if len(propertyComments) > 0 {
		err := dec.applyPropertyComments(context, path, propertyComments)
		if err != nil {
			return nil
		}
	}

	rhsNode := yqlib.CreateStringScalarNode(value)
	rhsNode.Tag = rhsNode.GuessTagFromCustomType()

	return dec.d.DeeplyAssign(context, path, rhsNode)
}

func (dec *propertiesDecoder) Decode() (*yqlib.CandidateNode, error) {
	if dec.finished {
		return nil, io.EOF
	}
	buf := new(bytes.Buffer)

	if _, err := buf.ReadFrom(dec.reader); err != nil {
		return nil, err
	}
	if buf.Len() == 0 {
		dec.finished = true
		return nil, io.EOF
	}
	properties, err := properties.LoadString(buf.String())
	if err != nil {
		return nil, err
	}
	properties.DisableExpansion = true

	rootMap := &yqlib.CandidateNode{
		Kind: yqlib.MappingNode,
		Tag:  "!!map",
	}

	context := yqlib.Context{}
	context = context.SingleChildContext(rootMap)

	for _, key := range properties.Keys() {
		if err := dec.applyProperty(context, properties, key); err != nil {
			return nil, err
		}

	}
	dec.finished = true

	return rootMap, nil

}
