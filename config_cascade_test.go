package main

import "testing"

func TestResolveCascadingConfigKeepsGatesStrict(t *testing.T) {
	files := map[string][]byte{
		".px1/config.json":         []byte(`{"version":1,"strictness":3,"failOnP0":true,"maxInlineComments":10,"disabledRules":["root-rule"]}`),
		"src/.px1/config.json":     []byte(`{"version":1,"strictness":1,"failOnP0":false,"maxInlineComments":20,"commentTypes":["logic"],"disabledRules":["child-rule"]}`),
		"src/api/.px1/config.json": []byte(`{"version":1,"maxInlineComments":5}`),
	}
	got, err := resolveCascadingConfig("src/api/handler.go", files)
	if err != nil {
		t.Fatal(err)
	}
	if got.Strictness != 3 || !got.FailOnP0 || got.MaxInlineComments != 5 || len(got.CommentTypes) != 1 || len(got.DisabledRules) != 2 || len(got.Sources) != 3 {
		t.Fatalf("resolved = %#v", got)
	}
}

func TestCascadingConfigCollectsBoundedContextRepositories(t *testing.T) {
	files := map[string][]byte{
		".px1/config.json":     []byte(`{"version":1,"context":{"repos":["Acme/API"]}}`),
		"src/.px1/config.json": []byte(`{"version":1,"context":{"repos":["acme/api","acme/web"]}}`),
	}
	config, err := resolveCascadingConfig("src/handler.go", files)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.ContextRepos) != 2 || config.ContextRepos[0] != "acme/api" || config.ContextRepos[1] != "acme/web" {
		t.Fatalf("context repos = %#v", config.ContextRepos)
	}
	if _, err := decodeConfig([]byte(`{"version":1,"context":{"repos":["../private"]}}`)); err == nil {
		t.Fatal("accepted invalid context repository")
	}
}

func TestResolveCascadingConfigRejectsUnknownFields(t *testing.T) {
	_, err := resolveCascadingConfig("a.go", map[string][]byte{".px1/config.json": []byte(`{"version":1,"surprise":true}`)})
	if err == nil {
		t.Fatal("unknown config field was accepted")
	}
}
