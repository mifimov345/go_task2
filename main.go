package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ValidationError struct {
	Line int
	Msg  string
}

var (
	snakeCaseRe = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)
	memoryRe    = regexp.MustCompile(`^[0-9]+(Gi|Mi|Ki)$`)
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(1)
	}
	filename := os.Args[1]
	shortName := filepath.Base(filename)
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	errors := validatePod(&root)
	if len(errors) > 0 {
		for _, e := range errors {
			if e.Line == 0 {
				fmt.Println(e.Msg)
			} else {
				fmt.Printf("%s:%d %s\n", shortName, e.Line, e.Msg)
			}
		}
		os.Exit(1)
	}
}

func validatePod(root *yaml.Node) []ValidationError {
	var errs []ValidationError
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		errs = append(errs, ValidationError{
			Msg: "document is required",
		})
		return errs
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		errs = append(errs, ValidationError{
			Line: doc.Line,
			Msg:  "document must be object",
		})
		return errs
	}
	apiKey, apiVal := getMapField(doc, "apiVersion")
	if apiKey == nil {
		errs = append(errs, ValidationError{Msg: "apiVersion is required"})
	} else {
		if !isStringScalar(apiVal) {
			errs = append(errs, ValidationError{
				Line: apiKey.Line,
				Msg:  "apiVersion must be string",
			})
		} else if apiVal.Value != "v1" {
			errs = append(errs, ValidationError{
				Line: apiKey.Line,
				Msg:  fmt.Sprintf("apiVersion has unsupported value '%s'", apiVal.Value),
			})
		}
	}
	kindKey, kindVal := getMapField(doc, "kind")
	if kindKey == nil {
		errs = append(errs, ValidationError{Msg: "kind is required"})
	} else {
		if !isStringScalar(kindVal) {
			errs = append(errs, ValidationError{
				Line: kindKey.Line,
				Msg:  "kind must be string",
			})
		} else if kindVal.Value != "Pod" {
			errs = append(errs, ValidationError{
				Line: kindKey.Line,
				Msg:  fmt.Sprintf("kind has unsupported value '%s'", kindVal.Value),
			})
		}
	}
	metadataKey, metadataVal := getMapField(doc, "metadata")
	if metadataKey == nil {
		errs = append(errs, ValidationError{Msg: "metadata is required"})
	} else {
		if metadataVal.Kind != yaml.MappingNode {
			errs = append(errs, ValidationError{
				Line: metadataKey.Line,
				Msg:  "metadata must be object",
			})
		} else {
			validateMetadata(metadataVal, &errs)
		}
	}
	specKey, specVal := getMapField(doc, "spec")
	if specKey == nil {
		errs = append(errs, ValidationError{Msg: "spec is required"})
	} else {
		if specVal.Kind != yaml.MappingNode {
			errs = append(errs, ValidationError{
				Line: specKey.Line,
				Msg:  "spec must be object",
			})
		} else {
			validateSpec(specVal, &errs)
		}
	}
	return errs
}

func validateMetadata(node *yaml.Node, errs *[]ValidationError) {
	nameKey, nameVal := getMapField(node, "name")
	if nameKey == nil {
		*errs = append(*errs, ValidationError{Msg: "name is required"})
	} else if !isStringScalar(nameVal) {
		*errs = append(*errs, ValidationError{
			Line: nameKey.Line,
			Msg:  "name must be string",
		})
	} else if nameVal.Value == "" {
		*errs = append(*errs, ValidationError{
			Line: nameKey.Line,
			Msg:  "name is required",
		})
	}
	if nsKey, nsVal := getMapField(node, "namespace"); nsKey != nil {
		if !isStringScalar(nsVal) {
			*errs = append(*errs, ValidationError{
				Line: nsKey.Line,
				Msg:  "namespace must be string",
			})
		}
	}
	if labelsKey, labelsVal := getMapField(node, "labels"); labelsKey != nil {
		if labelsVal.Kind != yaml.MappingNode {
			*errs = append(*errs, ValidationError{
				Line: labelsKey.Line,
				Msg:  "labels must be object",
			})
		}
	}
}

func validateSpec(node *yaml.Node, errs *[]ValidationError) {
	if osKey, osVal := getMapField(node, "os"); osKey != nil {
		if !isStringScalar(osVal) {
			*errs = append(*errs, ValidationError{
				Line: osKey.Line,
				Msg:  "os must be string",
			})
		} else if osVal.Value != "linux" && osVal.Value != "windows" {
			*errs = append(*errs, ValidationError{
				Line: osKey.Line,
				Msg:  fmt.Sprintf("os has unsupported value '%s'", osVal.Value),
			})
		}
	}
	contKey, contVal := getMapField(node, "containers")
	if contKey == nil {
		*errs = append(*errs, ValidationError{Msg: "containers is required"})
		return
	}
	if contVal.Kind != yaml.SequenceNode {
		*errs = append(*errs, ValidationError{
			Line: contKey.Line,
			Msg:  "containers must be array",
		})
		return
	}
	for _, c := range contVal.Content {
		if c.Kind != yaml.MappingNode {
			*errs = append(*errs, ValidationError{
				Line: c.Line,
				Msg:  "container must be object",
			})
			continue
		}
		validateContainer(c, errs)
	}
}

func validateContainer(node *yaml.Node, errs *[]ValidationError) {
	nameKey, nameVal := getMapField(node, "name")
	if nameKey == nil {
		*errs = append(*errs, ValidationError{Msg: "name is required"})
	} else {
		if !isStringScalar(nameVal) {
			*errs = append(*errs, ValidationError{
				Line: nameKey.Line,
				Msg:  "name must be string",
			})
		} else if !snakeCaseRe.MatchString(nameVal.Value) {
			*errs = append(*errs, ValidationError{
				Line: nameKey.Line,
				Msg:  fmt.Sprintf("name has invalid format '%s'", nameVal.Value),
			})
		}
	}
	imageKey, imageVal := getMapField(node, "image")
	if imageKey == nil {
		*errs = append(*errs, ValidationError{Msg: "image is required"})
	} else {
		if !isStringScalar(imageVal) {
			*errs = append(*errs, ValidationError{
				Line: imageKey.Line,
				Msg:  "image must be string",
			})
		} else if !isValidImage(imageVal.Value) {
			*errs = append(*errs, ValidationError{
				Line: imageKey.Line,
				Msg:  fmt.Sprintf("image has invalid format '%s'", imageVal.Value),
			})
		}
	}
	if portsKey, portsVal := getMapField(node, "ports"); portsKey != nil {
		if portsVal.Kind != yaml.SequenceNode {
			*errs = append(*errs, ValidationError{
				Line: portsKey.Line,
				Msg:  "ports must be array",
			})
		} else {
			for _, p := range portsVal.Content {
				if p.Kind != yaml.MappingNode {
					*errs = append(*errs, ValidationError{
						Line: p.Line,
						Msg:  "port must be object",
					})
					continue
				}
				validateContainerPort(p, errs)
			}
		}
	}
	if rpKey, rpVal := getMapField(node, "readinessProbe"); rpKey != nil {
		if rpVal.Kind != yaml.MappingNode {
			*errs = append(*errs, ValidationError{
				Line: rpKey.Line,
				Msg:  "readinessProbe must be object",
			})
		} else {
			validateProbe(rpVal, errs)
		}
	}
	if lpKey, lpVal := getMapField(node, "livenessProbe"); lpKey != nil {
		if lpVal.Kind != yaml.MappingNode {
			*errs = append(*errs, ValidationError{
				Line: lpKey.Line,
				Msg:  "livenessProbe must be object",
			})
		} else {
			validateProbe(lpVal, errs)
		}
	}
	resKey, resVal := getMapField(node, "resources")
	if resKey == nil {
		*errs = append(*errs, ValidationError{Msg: "resources is required"})
	} else {
		if resVal.Kind != yaml.MappingNode {
			*errs = append(*errs, ValidationError{
				Line: resKey.Line,
				Msg:  "resources must be object",
			})
		} else {
			validateResources(resVal, errs)
		}
	}
}

func validateContainerPort(node *yaml.Node, errs *[]ValidationError) {
	cpKey, cpVal := getMapField(node, "containerPort")
	if cpKey == nil {
		*errs = append(*errs, ValidationError{Msg: "containerPort is required"})
	} else {
		if !isIntScalar(cpVal) {
			*errs = append(*errs, ValidationError{
				Line: cpKey.Line,
				Msg:  "containerPort must be int",
			})
		} else {
			port, _ := strconv.Atoi(cpVal.Value)
			if port <= 0 || port >= 65536 {
				*errs = append(*errs, ValidationError{
					Line: cpKey.Line,
					Msg:  "containerPort value out of range",
				})
			}
		}
	}
	if protoKey, protoVal := getMapField(node, "protocol"); protoKey != nil {
		if !isStringScalar(protoVal) {
			*errs = append(*errs, ValidationError{
				Line: protoKey.Line,
				Msg:  "protocol must be string",
			})
		} else if protoVal.Value != "TCP" && protoVal.Value != "UDP" {
			*errs = append(*errs, ValidationError{
				Line: protoKey.Line,
				Msg:  fmt.Sprintf("protocol has unsupported value '%s'", protoVal.Value),
			})
		}
	}
}
func validateProbe(node *yaml.Node, errs *[]ValidationError) {
	httpKey, httpVal := getMapField(node, "httpGet")
	if httpKey == nil {
		*errs = append(*errs, ValidationError{Msg: "httpGet is required"})
		return
	}
	if httpVal.Kind != yaml.MappingNode {
		*errs = append(*errs, ValidationError{
			Line: httpKey.Line,
			Msg:  "httpGet must be object",
		})
		return
	}
	pathKey, pathVal := getMapField(httpVal, "path")
	if pathKey == nil {
		*errs = append(*errs, ValidationError{Msg: "path is required"})
	} else {
		if !isStringScalar(pathVal) {
			*errs = append(*errs, ValidationError{
				Line: pathKey.Line,
				Msg:  "path must be string",
			})
		} else if !strings.HasPrefix(pathVal.Value, "/") {
			*errs = append(*errs, ValidationError{
				Line: pathKey.Line,
				Msg:  fmt.Sprintf("path has invalid format '%s'", pathVal.Value),
			})
		}
	}
	portKey, portVal := getMapField(httpVal, "port")
	if portKey == nil {
		*errs = append(*errs, ValidationError{Msg: "port is required"})
	} else {
		if !isIntScalar(portVal) {
			*errs = append(*errs, ValidationError{
				Line: portKey.Line,
				Msg:  "port must be int",
			})
		} else {
			port, _ := strconv.Atoi(portVal.Value)
			if port <= 0 || port >= 65536 {
				*errs = append(*errs, ValidationError{
					Line: portKey.Line,
					Msg:  "port value out of range",
				})
			}
		}
	}
}

func validateResources(node *yaml.Node, errs *[]ValidationError) {
	// limits (опционально)
	if limitsKey, limitsVal := getMapField(node, "limits"); limitsKey != nil {
		if limitsVal.Kind != yaml.MappingNode {
			*errs = append(*errs, ValidationError{
				Line: limitsKey.Line,
				Msg:  "limits must be object",
			})
		} else {
			validateResourceMap(limitsVal, errs)
		}
	}
	if reqKey, reqVal := getMapField(node, "requests"); reqKey != nil {
		if reqVal.Kind != yaml.MappingNode {
			*errs = append(*errs, ValidationError{
				Line: reqKey.Line,
				Msg:  "requests must be object",
			})
		} else {
			validateResourceMap(reqVal, errs)
		}
	}
}

func validateResourceMap(node *yaml.Node, errs *[]ValidationError) {
	if cpuKey, cpuVal := getMapField(node, "cpu"); cpuKey != nil {
		if !isIntScalar(cpuVal) {
			*errs = append(*errs, ValidationError{
				Line: cpuKey.Line,
				Msg:  "cpu must be int",
			})
		}
	}
	if memKey, memVal := getMapField(node, "memory"); memKey != nil {
		if !isStringScalar(memVal) {
			*errs = append(*errs, ValidationError{
				Line: memKey.Line,
				Msg:  "memory must be string",
			})
		} else if !memoryRe.MatchString(memVal.Value) {
			*errs = append(*errs, ValidationError{
				Line: memKey.Line,
				Msg:  fmt.Sprintf("memory has invalid format '%s'", memVal.Value),
			})
		}
	}
}

func getMapField(m *yaml.Node, field string) (keyNode, valueNode *yaml.Node) {
	if m.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		v := m.Content[i+1]
		if k.Value == field {
			return k, v
		}
	}
	return nil, nil
}

func isStringScalar(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!str"
}

func isIntScalar(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!int"
}

func isValidImage(s string) bool {
	const prefix = "registry.bigbrother.io/"
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	rest := s[len(prefix):]
	colon := strings.LastIndex(rest, ":")
	if colon == -1 {
		return false
	}
	tag := rest[colon+1:]
	return tag != ""
}
