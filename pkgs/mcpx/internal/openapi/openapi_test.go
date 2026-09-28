package openapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/openapi"
)

const petstore = `{
  "openapi": "3.0.0",
  "info": { "title": "Pet Store", "version": "1.0" },
  "servers": [{ "url": "https://api.example.com/v1" }],
  "paths": {
    "/pets": {
      "get": {
        "operationId": "listPets", "summary": "List all pets",
        "parameters": [
          { "name": "limit", "in": "query", "schema": { "type": "integer" },
            "description": "how many to return" },
          { "name": "tags", "in": "query", "schema": { "type": "array" } }
        ]
      },
      "post": {
        "operationId": "createPet", "summary": "Add a pet",
        "requestBody": { "required": true,
          "content": { "application/json": { "schema": { "type": "object" } } } }
      }
    },
    "/pets/{petId}": {
      "get": {
        "operationId": "getPet", "summary": "One pet",
        "parameters": [
          { "name": "petId", "in": "path", "required": true, "schema": { "type": "string" } },
          { "name": "X-Trace", "in": "header", "schema": { "type": "string" } }
        ]
      },
      "delete": { "operationId": "deletePet", "summary": "Remove a pet" }
    },
    "/legacy": { "get": { "operationId": "old", "deprecated": true } }
  }
}`

func build(t *testing.T, opt openapi.Options) *openapi.API {
	t.Helper()
	spec, err := openapi.Parse([]byte(petstore))
	if err != nil {
		t.Fatal(err)
	}
	api, err := openapi.Build(spec, opt)
	if err != nil {
		t.Fatal(err)
	}
	return api
}

func names(api *openapi.API) []string {
	var out []string
	for _, tool := range api.Tools {
		out = append(out, tool.Name)
	}
	return out
}

func TestOperationsBecomeTools(t *testing.T) {
	api := build(t, openapi.Options{})
	got := strings.Join(names(api), " ")
	for _, want := range []string{"pet_store_listpets", "pet_store_getpet"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from %q", want, got)
		}
	}
}

func TestWritesAreNotExposedUnlessAsked(t *testing.T) {
	// A specification describes what a service can do, not what you meant to
	// allow. The difference between listing orders and cancelling them should
	// be a deliberate keystroke.
	got := strings.Join(names(build(t, openapi.Options{})), " ")
	if strings.Contains(got, "createpet") || strings.Contains(got, "deletepet") {
		t.Errorf("POST and DELETE should be absent by default: %q", got)
	}
	all := strings.Join(names(build(t, openapi.Options{Methods: []string{"all"}})), " ")
	if !strings.Contains(all, "createpet") || !strings.Contains(all, "deletepet") {
		t.Errorf("and present when asked: %q", all)
	}
}

func TestDeprecatedOperationsAreSkipped(t *testing.T) {
	if strings.Contains(strings.Join(names(build(t, openapi.Options{})), " "), "_old") {
		t.Error("a deprecated operation should not appear by default")
	}
	api := build(t, openapi.Options{IncludeDeprecated: true})
	found := false
	for _, tool := range api.Tools {
		if strings.HasSuffix(tool.Name, "_old") {
			found = true
			if !strings.Contains(tool.Description, "deprecated") {
				t.Error("and should be labelled when it does")
			}
		}
	}
	if !found {
		t.Error("expected it when asked for")
	}
}

func TestParametersBecomeOneFlatSchema(t *testing.T) {
	// A tool call is one bag of named arguments; where each goes is the
	// caller's problem, not the model's.
	api := build(t, openapi.Options{})
	for _, tool := range api.Tools {
		if !strings.HasSuffix(tool.Name, "getpet") {
			continue
		}
		var doc struct {
			Type       string   `json:"type"`
			Required   []string `json:"required"`
			Properties map[string]struct {
				Type        string `json:"type"`
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &doc); err != nil {
			t.Fatal(err)
		}
		if doc.Type != "object" {
			t.Errorf("got %q", doc.Type)
		}
		if _, ok := doc.Properties["petId"]; !ok {
			t.Error("a path parameter should be an argument")
		}
		if _, ok := doc.Properties["X-Trace"]; !ok {
			t.Error("a header parameter should be too")
		}
		// Path parameters cannot be omitted, so saying where they go matters.
		if !strings.Contains(doc.Properties["petId"].Description, "path") {
			t.Errorf("it should say where it goes: %q", doc.Properties["petId"].Description)
		}
		if len(doc.Required) != 1 || doc.Required[0] != "petId" {
			t.Errorf("required = %v", doc.Required)
		}
		return
	}
	t.Fatal("getpet was not built")
}

func TestARequestBodyBecomesABodyArgument(t *testing.T) {
	api := build(t, openapi.Options{Methods: []string{"POST"}})
	var doc struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}
	_ = json.Unmarshal(api.Tools[0].InputSchema, &doc)
	if _, ok := doc.Properties["body"]; !ok {
		t.Errorf("expected a body argument: %v", doc.Properties)
	}
	if len(doc.Required) == 0 || doc.Required[0] != "body" {
		t.Errorf("a required body should be required: %v", doc.Required)
	}
}

func TestCallsGoToTheRightPlace(t *testing.T) {
	var gotPath, gotQuery, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotHeader = r.URL.Path, r.URL.RawQuery, r.Header.Get("X-Trace")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"rex"}`))
	}))
	defer srv.Close()

	api := build(t, openapi.Options{BaseURL: srv.URL})
	var tool string
	for _, tl := range api.Tools {
		if strings.HasSuffix(tl.Name, "getpet") {
			tool = tl.Name
		}
	}
	res, err := api.Call(context.Background(), tool, map[string]any{
		"petId": "123", "X-Trace": "abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/pets/123" {
		t.Errorf("path parameter not substituted: %q", gotPath)
	}
	if gotHeader != "abc" {
		t.Errorf("header parameter not sent: %q", gotHeader)
	}
	_ = gotQuery
	if res.Status != 200 || len(res.JSON) == 0 {
		t.Errorf("got %+v", res)
	}
}

func TestAPathParameterCannotSmuggleASlash(t *testing.T) {
	// Unescaped, a slash silently changes which endpoint is called.
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
	}))
	defer srv.Close()
	api := build(t, openapi.Options{BaseURL: srv.URL})
	var tool string
	for _, tl := range api.Tools {
		if strings.HasSuffix(tl.Name, "getpet") {
			tool = tl.Name
		}
	}
	if _, err := api.Call(context.Background(), tool, map[string]any{
		"petId": "../admin",
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotPath, "/admin") && !strings.Contains(gotPath, "%2F") {
		t.Errorf("the slash should have been escaped: %q", gotPath)
	}
}

func TestAnArrayQueryParameterRepeats(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
	}))
	defer srv.Close()
	api := build(t, openapi.Options{BaseURL: srv.URL})
	var tool string
	for _, tl := range api.Tools {
		if strings.HasSuffix(tl.Name, "listpets") {
			tool = tl.Name
		}
	}
	_, _ = api.Call(context.Background(), tool, map[string]any{"tags": []any{"a", "b"}})
	if strings.Count(gotQuery, "tags=") != 2 {
		t.Errorf("expected the parameter twice: %q", gotQuery)
	}
}

func TestANonSuccessStatusIsReturnedNotRaised(t *testing.T) {
	// The service answered; what the answer means is the caller's judgement.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()
	api := build(t, openapi.Options{BaseURL: srv.URL})
	res, err := api.Call(context.Background(), api.Tools[0].Name, map[string]any{"petId": "x"})
	if err != nil {
		t.Fatalf("a 404 is not a transport error: %v", err)
	}
	if res.Status != 404 {
		t.Errorf("got %d", res.Status)
	}
}

func TestYAMLIsAccepted(t *testing.T) {
	// Specifications are published in both and which one you get is not the
	// caller's choice.
	spec, err := openapi.Parse([]byte(`
openapi: 3.0.0
info:
  title: Yaml Api
  version: "1"
servers:
  - url: https://example.com
paths:
  /things:
    get:
      operationId: listThings
      summary: things
`))
	if err != nil {
		t.Fatal(err)
	}
	api, err := openapi.Build(spec, openapi.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(api.Tools) != 1 || !strings.Contains(api.Tools[0].Name, "listthings") {
		t.Errorf("got %v", names(api))
	}
}

func TestSwagger2IsUnderstood(t *testing.T) {
	// It spells the server differently, and refusing would exclude a large
	// amount of what is actually published.
	spec, err := openapi.Parse([]byte(`{
		"swagger": "2.0",
		"info": { "title": "Old", "version": "1" },
		"host": "old.example.com", "basePath": "/api", "schemes": ["https"],
		"paths": { "/x": { "get": { "operationId": "getX" } } }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	api, err := openapi.Build(spec, openapi.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if api.BaseURL != "https://old.example.com/api" {
		t.Errorf("got %q", api.BaseURL)
	}
	if !strings.Contains(spec.Version(), "swagger") {
		t.Errorf("the dialect should be reported: %q", spec.Version())
	}
}

func TestFilteringNarrowsALargeSpecification(t *testing.T) {
	api := build(t, openapi.Options{Include: []string{"pets/{"}})
	if len(api.Tools) != 1 {
		t.Errorf("include should narrow: %v", names(api))
	}
	api = build(t, openapi.Options{Exclude: []string{"petId"}})
	for _, n := range names(api) {
		if strings.Contains(n, "getpet") {
			t.Errorf("exclude should drop it: %v", names(api))
		}
	}
}

func TestNothingMatchingSaysWhatWasConsidered(t *testing.T) {
	spec, _ := openapi.Parse([]byte(petstore))
	_, err := openapi.Build(spec, openapi.Options{Include: []string{"nonexistent"}})
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "paths were considered") {
		t.Errorf("the error should be actionable: %v", err)
	}
}

func TestAMissingServerURLIsReportedWithTheFix(t *testing.T) {
	spec, _ := openapi.Parse([]byte(`{"openapi":"3.0.0","info":{"title":"X"},
		"paths":{"/a":{"get":{"operationId":"a"}}}}`))
	_, err := openapi.Build(spec, openapi.Options{})
	if err == nil || !strings.Contains(err.Error(), "--base-url") {
		t.Errorf("the error should name the remedy: %v", err)
	}
}

func TestARelativeServerURLResolvesAgainstWhereTheSpecCameFrom(t *testing.T) {
	// The specification permits it and real services use it -- the Swagger
	// petstore declares "/api/v3". Unresolved, the request has no scheme and
	// fails in a way that reads as a network problem.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/openapi.json") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"openapi":"3.0.0","info":{"title":"Rel"},
				"servers":[{"url":"/api/v3"}],
				"paths":{"/things":{"get":{"operationId":"listThings"}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"path":"` + r.URL.Path + `"}`))
	}))
	defer srv.Close()

	spec, err := openapi.Load(context.Background(), srv.URL+"/openapi.json", 0)
	if err != nil {
		t.Fatal(err)
	}
	api, err := openapi.Build(spec, openapi.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(api.BaseURL, srv.URL) {
		t.Fatalf("the relative URL should have resolved against the source: %q", api.BaseURL)
	}
	res, err := api.Call(context.Background(), api.Tools[0].Name, nil)
	if err != nil {
		t.Fatalf("the call should reach the server: %v", err)
	}
	if !strings.Contains(res.Body, "/api/v3/things") {
		t.Errorf("the base path should be kept: %s", res.Body)
	}
}

func TestARelativeURLWithNoHTTPSourceSaysWhatToDo(t *testing.T) {
	spec, _ := openapi.Parse([]byte(`{"openapi":"3.0.0","info":{"title":"X"},
		"servers":[{"url":"/api"}],"paths":{"/a":{"get":{"operationId":"a"}}}}`))
	_, err := openapi.Build(spec, openapi.Options{})
	if err == nil || !strings.Contains(err.Error(), "--base-url") {
		t.Errorf("the remedy should be named: %v", err)
	}
}
