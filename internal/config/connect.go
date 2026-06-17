package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// WriteAppAuth updates (or creates) a config file's github + auth sections to use
// GitHub App credentials, preserving other content and comments. Any existing
// auth.pat is removed.
func WriteAppAuth(path string, scope Scope, owner, repo string, appID, installationID int64, keyPath string) error {
	doc := loadOrNewMapping(path)

	gh := upsertMapping(doc, "github")
	setScalarIfAbsent(gh, "url", "https://github.com")
	setScalar(gh, "scope", string(scope))
	setScalar(gh, "owner", owner)
	if scope == ScopeRepo {
		setScalar(gh, "repo", repo)
	}

	auth := upsertMapping(doc, "auth")
	removeKey(auth, "pat")
	setInt(auth, "app_id", appID)
	setInt(auth, "installation_id", installationID)
	setScalar(auth, "private_key_path", keyPath)

	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

func loadOrNewMapping(path string) *yaml.Node {
	if data, err := os.ReadFile(path); err == nil {
		var root yaml.Node
		if err := yaml.Unmarshal(data, &root); err == nil &&
			len(root.Content) == 1 && root.Content[0].Kind == yaml.MappingNode {
			return root.Content[0]
		}
	}
	return &yaml.Node{Kind: yaml.MappingNode}
}

// findKey returns the value node and its key index for key, or (nil, -1).
func findKey(m *yaml.Node, key string) (*yaml.Node, int) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1], i
		}
	}
	return nil, -1
}

func upsertMapping(m *yaml.Node, key string) *yaml.Node {
	if v, _ := findKey(m, key); v != nil {
		if v.Kind != yaml.MappingNode {
			v.Kind = yaml.MappingNode
			v.Tag = "!!map"
			v.Content = nil
		}
		return v
	}
	v := &yaml.Node{Kind: yaml.MappingNode}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key}, v)
	return v
}

func setScalar(m *yaml.Node, key, value string) {
	if v, _ := findKey(m, key); v != nil {
		v.Kind = yaml.ScalarNode
		v.Tag = "!!str"
		v.Value = value
		return
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func setInt(m *yaml.Node, key string, value int64) {
	v := fmt.Sprintf("%d", value)
	if n, _ := findKey(m, key); n != nil {
		n.Kind = yaml.ScalarNode
		n.Tag = "!!int"
		n.Value = v
		return
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: v})
}

func setScalarIfAbsent(m *yaml.Node, key, value string) {
	if v, _ := findKey(m, key); v == nil {
		setScalar(m, key, value)
	}
}

func removeKey(m *yaml.Node, key string) {
	if _, idx := findKey(m, key); idx >= 0 {
		m.Content = append(m.Content[:idx], m.Content[idx+2:]...)
	}
}
