package main

import "testing"

func TestSanitizeRef(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// 用户名/路径含大写 → 整体转小写（老引擎拒绝大写仓库段）
		{"Leo123/docker.io:library.registry_2", "leo123/docker.io:library.registry_2"},
		{"LEO/docker.io:library.registry_2", "leo/docker.io:library.registry_2"},
		// 正常情况不变
		{"leo123/docker.io:library.registry_2", "leo123/docker.io:library.registry_2"},
		{"ccr.ccs.tencentyun.com/ns/docker.io:library.registry_2",
			"ccr.ccs.tencentyun.com/ns/docker.io:library.registry_2"},
		// tag 非法字符替换为 -
		{"user/repo:v1.0 beta", "user/repo:v1.0-beta"},
		// 无 tag 的引用
		{"Leo/repo", "leo/repo"},
	}
	for _, c := range cases {
		if got := SanitizeRef(c.in); got != c.want {
			t.Errorf("SanitizeRef(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
