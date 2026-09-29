package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	gitpkg "github.com/bxnnyg/matrixctrl/internal/git"
)

// The two cases of 2026-09-28, shaped like the real section files.

const matrixTools = `## The matrix-tools image, used in multiple components
matrixTools:
  # Details of the image to be used
  image:
    ## The host and (optional) port of the container image registry for this component.
    registry: ghcr.io
    ## The path in the registry where the container image is located
    repository: element-hq/ess-helm/matrix-tools
    ## The tag of the container image to use.
    tag: "0.7.3"
    ## Container digest to use.
    # digest:
  labels: {}
`

const redis = `redis:
  maxMemory: 40mb
  # Details of the image to be used
  image:
    ## The host and (optional) port of the container image registry for this component.
    registry: docker.io
    ## The path in the registry where the container image is located
    repository: library/redis
    ## The tag of the container image to use.
    tag: "7.4-alpine"
    ## A list of pull secrets to use for this image
    pullSecrets: []
  labels: {}
`

func TestCommentingOutATagKeepsEverythingElse(t *testing.T) {
	out, done, err := CommentOut(matrixTools, []string{"matrixTools", "image", "tag"})
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 || done[0] != "matrixTools.image.tag" {
		t.Errorf("done %v", done)
	}
	if !strings.Contains(out, `    # tag: "0.7.3"  # folgt dem Chart`) {
		t.Errorf("the tag line should be commented in place:\n%s", out)
	}
	// Only that line changed.
	if a, b := strings.Split(matrixTools, "\n"), strings.Split(out, "\n"); len(a) != len(b) {
		t.Fatalf("line count changed")
	} else {
		changed := 0
		for i := range a {
			if a[i] != b[i] {
				changed++
			}
		}
		if changed != 1 {
			t.Errorf("%d lines changed, want 1", changed)
		}
	}
	values := parse(t, out)["matrixTools"].(map[string]interface{})["image"].(map[string]interface{})
	if _, ok := values["tag"]; ok || values["repository"] != "element-hq/ess-helm/matrix-tools" {
		t.Errorf("the tag should be gone and the rest intact: %v", values)
	}
}

// The redis case: commenting out only the tag would still leave registry, repository
// and pullSecrets — so the whole block is asked for, and it goes as a block.
func TestCommentingOutABlockTakesAllOfIt(t *testing.T) {
	out, _, err := CommentOut(redis, []string{"redis", "image"})
	if err != nil {
		t.Fatal(err)
	}
	r := parse(t, out)["redis"].(map[string]interface{})
	if _, ok := r["image"]; ok {
		t.Errorf("image should be gone entirely:\n%s", out)
	}
	if r["maxMemory"] != "40mb" || r["labels"] == nil {
		t.Errorf("the siblings must survive: %v", r)
	}
}

// Yesterday's second failure in small: the parent left with only comments becomes
// `image: null`, which in Helm removes the chart's image defaults. The parent has to
// go with it. The counter-probe is the matrixTools case above, where the parent keeps
// registry and repository and must stay.
func TestAParentLeftEmptyGoesToo(t *testing.T) {
	src := "hookshot:\n  image:\n    ## doc\n    tag: \"7.3.2\"\n  enabled: false\n"
	out, done, err := CommentOut(src, []string{"hookshot", "image", "tag"})
	if err != nil {
		t.Fatal(err)
	}
	h := parse(t, out)["hookshot"].(map[string]interface{})
	if _, ok := h["image"]; ok {
		t.Errorf("an empty image block would null the chart's image — it must be commented out too:\n%s", out)
	}
	if h["enabled"] != false {
		t.Errorf("sibling lost: %v", h)
	}
	if len(done) != 2 {
		t.Errorf("done %v, want tag and image", done)
	}
}

func TestFlowStyleIsRefusedRatherThanBroken(t *testing.T) {
	src := "x:\n  image: {repository: a, tag: \"1\"}\n"
	if _, _, err := CommentOut(src, []string{"x", "image", "tag"}); err == nil {
		t.Error("a key sharing its line with others cannot be commented out alone")
	}
}

func TestAMissingKeyIsAnError(t *testing.T) {
	if _, _, err := CommentOut(matrixTools, []string{"matrixTools", "image", "digest"}); err == nil {
		t.Error("digest is only a comment here — there is nothing to comment out")
	}
}

func parse(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := yaml.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("result is not valid YAML: %v\n%s", err, s)
	}
	return m
}

// Through the store, with the two section files of 2026-09-28: the component is found
// in whichever file holds it, the right path is commented out, and the other file is
// not touched.
func TestFollowChartThroughTheStore(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "config-slices.json"),
		[]byte(`{"slices":[{"name":"general","file":"general.yaml"},{"name":"redis","file":"redis.yaml"}]}`), 0o644))
	must(os.WriteFile(filepath.Join(dir, "general.yaml"), []byte(matrixTools), 0o644))
	must(os.WriteFile(filepath.Join(dir, "redis.yaml"), []byte(redis), 0o644))
	repo, err := gitpkg.OpenOrInit(dir)
	must(err)
	s := NewStore(dir, repo)
	ctx := context.Background()

	done, err := s.FollowChart(ctx, "redis", true)
	must(err)
	if len(done) != 1 || done[0] != "redis.yaml: redis.image" {
		t.Errorf("done %v", done)
	}
	if g, _ := os.ReadFile(filepath.Join(dir, "general.yaml")); string(g) != matrixTools {
		t.Error("a file that does not hold the component was changed")
	}
	done, err = s.FollowChart(ctx, "matrixTools", false)
	must(err)
	if len(done) != 1 || done[0] != "general.yaml: matrixTools.image.tag" {
		t.Errorf("done %v", done)
	}
	if _, err := s.FollowChart(ctx, "hookshot", false); err == nil {
		t.Error("a component in no file is an error, not silence")
	}
}

// `additional: {}` filled in: a block, and a multi-line config as a literal block —
// not one line of escaped newlines (etappe 113).
func TestFillingAnEmptyFlowMapWritesABlock(t *testing.T) {
	src := "matrixAuthenticationService:\n  ## docs\n  additional: {}\n  labels: {}\n"
	n, _ := ParseYAMLNode(src)
	if err := SetNodeValue(n, []string{"matrixAuthenticationService", "additional", "matrixctrl-tasks", "config"}, "account:\n  password_registration_enabled: true\n"); err != nil {
		t.Fatal(err)
	}
	out, _ := MarshalNode(n)
	want := "matrixAuthenticationService:\n  ## docs\n  additional:\n    matrixctrl-tasks:\n      config: |\n        account:\n          password_registration_enabled: true\n  labels: {}\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}
