package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	trailingCommaRe = regexp.MustCompile(`,\s*([}\]])`)
	codeFenceRe     = regexp.MustCompile(`(?s)^\s*` + "`" + `{3}[a-zA-Z]*\s*(.*?)\s*` + "`" + `{3}\s*$`)
)

// ParseImages 解析 --content 参数，按优先级依次尝试：
//  1. 标准 JSON：{ "hub-mirror": ["a", "b"] }
//  2. 有瑕疵的 JSON：单引号、尾逗号、无引号键名或元素、裸数组、任意键名、markdown 代码块包裹
//  3. 纯文本：换行/逗号/空白分隔的镜像列表，支持 # 注释、- 列表符、YAML 风格的 "hub-mirror:" 头
func ParseImages(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("content is empty")
	}

	// 去掉 markdown 代码块包裹
	if m := codeFenceRe.FindStringSubmatch(raw); m != nil {
		raw = strings.TrimSpace(m[1])
	}

	// 1. 标准解析
	var hubMirrors struct {
		Content []string `json:"hub-mirror"`
	}
	if err := json.Unmarshal([]byte(raw), &hubMirrors); err == nil && len(hubMirrors.Content) > 0 {
		return NormalizeImages(hubMirrors.Content), nil
	}

	// 2. 容错解析
	if list := parseFlawedJSON(raw); len(list) > 0 {
		return NormalizeImages(list), nil
	}

	// 3. 纯文本解析
	if list := parsePlainText(raw); len(list) > 0 {
		return NormalizeImages(list), nil
	}

	return nil, fmt.Errorf("cannot parse image list from content: %q", raw)
}

// parseFlawedJSON 修复常见 JSON 瑕疵后解析
func parseFlawedJSON(raw string) []string {
	s := normalizeQuotes(raw)
	s = strings.TrimSpace(trailingCommaRe.ReplaceAllString(s, "$1"))

	// 整体是数组，或对象里嵌着数组
	if list := tryParseJSONArray(s); len(list) > 0 {
		return list
	}

	if strings.HasPrefix(s, "{") {
		// 任意键名：取对象中第一个字符串数组类型的值，
		// 兼容 hub_mirror / HubMirror / images 等写法，优先 hub-mirror
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(s), &obj); err == nil {
			var fallback []string
			for k, v := range obj {
				var arr []string
				if err := json.Unmarshal(v, &arr); err != nil || len(arr) == 0 {
					continue
				}
				if normalizeKey(k) == "hubmirror" {
					return arr
				}
				if fallback == nil {
					fallback = arr
				}
			}
			return fallback
		}

		// 键名没加引号等情况：直接抠出 [ ... ] 段再试
		if sub := extractArray(s); sub != "" && sub != s {
			if list := tryParseJSONArray(sub); len(list) > 0 {
				return list
			}
		}
	}
	return nil
}

// tryParseJSONArray 解析数组，元素带引号或不带引号均可
func tryParseJSONArray(s string) []string {
	if !strings.HasPrefix(s, "[") {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		return arr
	}
	// 元素没加引号: [a, b, c]
	return splitBareArray(s)
}

// splitBareArray 解析无引号元素的数组段
func splitBareArray(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	}) {
		f = strings.Trim(strings.TrimSpace(f), `"'`)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// normalizeQuotes 把不在双引号字符串内部的 ' 换成 "，
// 镜像名里不会出现单引号，这样 'a','b' 这类写法就能被标准解析
func normalizeQuotes(s string) string {
	var b strings.Builder
	inDouble := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && inDouble && i+1 < len(s):
			b.WriteByte(c)
			i++
			b.WriteByte(s[i])
		case c == '"':
			inDouble = !inDouble
			b.WriteByte(c)
		case c == '\'' && !inDouble:
			b.WriteByte('"')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// extractArray 抠出第一个 [ 到最后一个 ] 的子串
func extractArray(s string) string {
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start != -1 && end > start {
		return s[start : end+1]
	}
	return ""
}

// normalizeKey 统一键名比较：去掉 - _ . 和大小写
func normalizeKey(k string) string {
	k = strings.NewReplacer("-", "", "_", "", ".", "").Replace(k)
	return strings.ToLower(k)
}

// parsePlainText 把内容当作纯文本镜像列表：按换行/逗号/空白切分
func parsePlainText(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		for _, f := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ';' || r == '\t' || r == ' '
		}) {
			f = strings.Trim(strings.TrimSpace(f), `"'`)
			f = strings.TrimPrefix(strings.TrimPrefix(f, "-"), "-")
			f = strings.TrimSpace(f)
			// 跳过空项、JSON 残留符号、YAML 键头（如 "hub-mirror:"）
			if f == "" || strings.HasPrefix(f, "#") || strings.HasPrefix(f, "//") ||
				f == "{" || f == "}" || f == "[" || f == "]" ||
				(strings.HasSuffix(f, ":") && !strings.Contains(f, "/")) {
				continue
			}
			out = append(out, f)
		}
	}
	return out
}

// NormalizeImages 规范化镜像列表：去空、去重、为短镜像名补全 docker.io 前缀
func NormalizeImages(list []string) []string {
	seen := make(map[string]struct{}, len(list))
	out := make([]string, 0, len(list))
	for _, v := range list {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		v = NormalizeImage(v)
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// NormalizeImage 补全镜像全名：
//   - nginx            -> docker.io/library/nginx:latest
//   - redis:6.0        -> docker.io/library/redis:6.0
//   - user/repo        -> docker.io/user/repo:latest
//   - gcr.io/a/b:v1    -> gcr.io/a/b:v1
//
// 与 docker 的规则一致：没有 registry（首段不含 . 或 :，也不是 localhost）
// 就补 docker.io；官方镜像（只有一段路径）补 library/。
func NormalizeImage(image string) string {
	image = strings.TrimSpace(image)
	image = strings.TrimPrefix(image, "docker://")
	// digest 形式 name@sha256:...，只规范 name 部分，不补 tag
	if i := strings.Index(image, "@"); i != -1 {
		return normalizeName(image[:i], false) + strings.ToLower(image[i:])
	}
	return normalizeName(image, true)
}

// normalizeName 处理不含 digest 的镜像名；defaultTag 为 true 时缺省补 :latest
func normalizeName(name string, defaultTag bool) string {
	// 分离 name 与 tag（最后一段冒号且冒号后无 / 才是 tag）
	tag := ""
	if i := strings.LastIndex(name, ":"); i != -1 && !strings.Contains(name[i+1:], "/") {
		tag = name[i+1:]
		name = name[:i]
	}

	registry := ""
	path := name
	if i := strings.Index(name, "/"); i != -1 {
		first := name[:i]
		// 首段含 . 或 : 或是 localhost，视为 registry
		if strings.ContainsAny(first, ".:") || first == "localhost" {
			registry = strings.ToLower(first)
			path = name[i+1:]
		}
	}

	// docker.io 官方镜像（无斜杠路径）补 library/，其他私有仓库不补
	if !strings.Contains(path, "/") && (registry == "" || registry == "docker.io") {
		path = "library/" + path
	}

	full := path
	if registry != "" {
		full = registry + "/" + path
	} else {
		full = "docker.io/" + path
	}
	if tag == "" {
		if !defaultTag {
			return full
		}
		tag = "latest"
	}
	return full + ":" + strings.ToLower(tag)
}

func dedupe(list []string) []string {
	seen := make(map[string]struct{}, len(list))
	out := make([]string, 0, len(list))
	for _, v := range list {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
