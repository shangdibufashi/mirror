package main

import (
	"reflect"
	"testing"
)

func TestParseImages(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"标准JSON", `{ "hub-mirror": ["gcr.io/a/b:v1", "hello-world:latest"] }`,
			[]string{"gcr.io/a/b:v1", "docker.io/library/hello-world:latest"}},
		{"单引号", `{ 'hub-mirror': ['gcr.io/a/b:v1', 'hello-world'] }`,
			[]string{"gcr.io/a/b:v1", "docker.io/library/hello-world:latest"}},
		{"尾逗号", `{ "hub-mirror": ["gcr.io/a/b:v1", "hello-world",] }`,
			[]string{"gcr.io/a/b:v1", "docker.io/library/hello-world:latest"}},
		{"裸数组", `["gcr.io/a/b:v1", "hello-world"]`,
			[]string{"gcr.io/a/b:v1", "docker.io/library/hello-world:latest"}},
		{"裸数组无引号", `[gcr.io/a/b:v1, hello-world]`,
			[]string{"gcr.io/a/b:v1", "docker.io/library/hello-world:latest"}},
		{"无引号键名", `{ hub-mirror: ["gcr.io/a/b:v1"] }`,
			[]string{"gcr.io/a/b:v1"}},
		{"任意键名", `{ "images": ["gcr.io/a/b:v1"] }`,
			[]string{"gcr.io/a/b:v1"}},
		{"下划线键名", `{ "hub_mirror": ["gcr.io/a/b:v1"] }`,
			[]string{"gcr.io/a/b:v1"}},
		{"markdown代码块", "```json\n{ \"hub-mirror\": [\"gcr.io/a/b:v1\"] }\n```",
			[]string{"gcr.io/a/b:v1"}},
		{"纯文本换行", "gcr.io/a/b:v1\nhello-world:latest",
			[]string{"gcr.io/a/b:v1", "docker.io/library/hello-world:latest"}},
		{"纯文本逗号", "gcr.io/a/b:v1, hello-world:latest",
			[]string{"gcr.io/a/b:v1", "docker.io/library/hello-world:latest"}},
		{"带注释和列表符", "# 镜像列表\n- gcr.io/a/b:v1\n- hello-world:latest",
			[]string{"gcr.io/a/b:v1", "docker.io/library/hello-world:latest"}},
		{"YAML风格", "hub-mirror:\n  - gcr.io/a/b:v1\n  - hello-world",
			[]string{"gcr.io/a/b:v1", "docker.io/library/hello-world:latest"}},
		{"对象包裹的瑕疵JSON", "{ 'hub-mirror': ['a:v1', 'b:v2',] }",
			[]string{"docker.io/library/a:v1", "docker.io/library/b:v2"}},
		{"空元素过滤和去重", `{ "hub-mirror": ["a:v1", "", "a:v1"] }`,
			[]string{"docker.io/library/a:v1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseImages(c.in)
			if err != nil {
				t.Fatalf("ParseImages(%q) error: %v", c.in, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseImages(%q)\n got: %#v\nwant: %#v", c.in, got, c.want)
			}
		})
	}

	for _, bad := range []string{"", "   "} {
		if _, err := ParseImages(bad); err == nil {
			t.Errorf("ParseImages(%q) should fail", bad)
		}
	}
}

func TestNormalizeImage(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"redis:6.0", "docker.io/library/redis:6.0"},
		{"nginx", "docker.io/library/nginx:latest"},
		{"nginx:latest", "docker.io/library/nginx:latest"},
		{"user/repo", "docker.io/user/repo:latest"},
		{"user/repo:v1", "docker.io/user/repo:v1"},
		{"gcr.io/a/b:v1", "gcr.io/a/b:v1"},
		{"quay.io/a/b", "quay.io/a/b:latest"},
		{"k8s.gcr.io/a/b:v1", "k8s.gcr.io/a/b:v1"},
		{"localhost:5000/repo:v1", "localhost:5000/repo:v1"},
		{"localhost/repo:v1", "localhost/repo:v1"},
		{"registry.cn-hangzhou.aliyuncs.com/ns/repo", "registry.cn-hangzhou.aliyuncs.com/ns/repo:latest"},
		{"GCR.IO/a/b:v1", "gcr.io/a/b:v1"},
		{"redis@sha256:abc123", "docker.io/library/redis@sha256:abc123"},
		{"gcr.io/a/b@sha256:abc123", "gcr.io/a/b@sha256:abc123"},
	}
	for _, c := range cases {
		if got := NormalizeImage(c.in); got != c.want {
			t.Errorf("NormalizeImage(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseImagesNormalize(t *testing.T) {
	// 短镜像名自动补全 docker.io 前缀
	got, err := ParseImages(`{ "hub-mirror": ["redis:6.0", "redis:6.0", "gcr.io/a/b:v1"] }`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docker.io/library/redis:6.0", "gcr.io/a/b:v1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}

	// 纯文本里的短镜像名同样补全
	got, err = ParseImages("redis:6.0\nnginx")
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"docker.io/library/redis:6.0", "docker.io/library/nginx:latest"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}
