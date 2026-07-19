package arctic

import "testing"

func TestCreateRepoPayloadSplitsNamespace(t *testing.T) {
	p := createRepoPayload("open-index/arctic", false)
	if p["organization"] != "open-index" {
		t.Errorf("organization = %v, want open-index", p["organization"])
	}
	if p["name"] != "arctic" {
		t.Errorf("name = %v, want arctic (no slash)", p["name"])
	}
	if p["type"] != "dataset" {
		t.Errorf("type = %v, want dataset", p["type"])
	}
	if p["private"] != false {
		t.Errorf("private = %v, want false", p["private"])
	}
}

func TestCreateRepoPayloadBareName(t *testing.T) {
	p := createRepoPayload("arctic", true)
	if _, ok := p["organization"]; ok {
		t.Errorf("bare name should not set organization, got %v", p["organization"])
	}
	if p["name"] != "arctic" {
		t.Errorf("name = %v, want arctic", p["name"])
	}
	if p["private"] != true {
		t.Errorf("private = %v, want true", p["private"])
	}
}
