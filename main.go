package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"text/template"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/spf13/pflag"
)

var (
	content    = pflag.StringP("content", "", "", "原始镜像，格式为：{ \"hub-mirror\": [] }")
	maxContent = pflag.IntP("maxContent", "", 10, "原始镜像个数限制")
	username   = pflag.StringP("username", "", "", "docker hub 用户名")
	password   = pflag.StringP("password", "", "", "docker hub 密码")
	outputPath = pflag.StringP("outputPath", "", "output.sh", "结果输出路径")
	repository = pflag.StringP("repository", "", "", "仓库地址,如果为空,默认推到dockerHub")
)
func FormatName(repository string, str string) string {
	arr := strings.Split(str, "/")
	hasDot := strings.Index(arr[0], ".") != -1
	tagName := "docker.io"
	version := strings.Join(arr, ".")
	if hasDot {
		tagName = arr[0]
		version = strings.Join(arr[1:], ".")
	}
	version = strings.ReplaceAll(version, ":", "_")
	results := repository + "/" + tagName + ":" + version
	return results
}

// SanitizeRef 把镜像引用修正为 docker 引擎能接受的格式：
//  - 去掉误配置进 secret 的 http:// / https:// 协议头（镜像引用不允许带协议）
//  - 折叠重复的 /（secret 末尾多带一个 / 时会产生 //，引擎会拒绝）
//  - 去掉首尾多余的 /
//  - 仓库路径各段转为小写（docker 要求 name 组件必须小写，用户名/仓库名含大写时会报
//    invalid reference format）
//  - tag 中的非法字符（. 除外的一律允许字母数字、_、-）不在白名单时替换为 -
//    这里 FormatName 生成的 tag 形如 library.registry_2，其中 . 是合法的
func SanitizeRef(ref string) string {
	// 协议头先去掉，否则里面的 // 会被折叠成单斜杠残留 http:/ 前缀
	ref = strings.TrimPrefix(ref, "https://")
	ref = strings.TrimPrefix(ref, "http://")
	// 再折叠 // 为 /，并去掉首尾 /，避免 "user//docker.io" 这类格式
	for strings.Contains(ref, "//") {
		ref = strings.ReplaceAll(ref, "//", "/")
	}
	ref = strings.Trim(ref, "/")
	i := -1
	// 找 tag 分隔：最后一个冒号且其后不含 /
	for j := len(ref) - 1; j >= 0; j-- {
		if ref[j] == ':' {
			if !strings.Contains(ref[j+1:], "/") {
				i = j
			}
			break
		}
		if ref[j] == '/' {
			break
		}
	}
	repo, tag := ref, ""
	if i != -1 {
		repo, tag = ref[:i], ref[i+1:]
	}
	// 仓库部分整体小写
	repo = strings.ToLower(repo)
	// tag 白名单：字母数字、_、-、.
	tag = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '-', r == '.':
			return r
		default:
			return '-'
		}
	}, tag)
	if tag != "" {
		return repo + ":" + tag
	}
	return repo
}
func main() {
	pflag.Parse()

	fmt.Println("验证原始镜像内容")
	hubMirrors, err := ParseImages(*content)
	if err != nil {
		panic(err)
	}
	content := struct {
		Content []string
	}{Content: hubMirrors}
	if len(content.Content) > *maxContent {
		panic("content is too long.")
	}
	fmt.Printf("%+v\n", content)

	fmt.Println("连接 Docker")
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}

	fmt.Println("验证 Docker 用户名密码")
	if *username == "" || *password == "" {
		panic("username or password cannot be empty.")
	}
	authConfig := types.AuthConfig{
		Username: *username,
		Password: *password,
		ServerAddress: *repository,
	}
	encodedJSON, err := json.Marshal(authConfig)
	if err != nil {
		panic(err)
	}
	authStr := base64.URLEncoding.EncodeToString(encodedJSON)
	_, err = cli.RegistryLogin(context.Background(), authConfig)
	if err != nil {
		panic(err)
	}

	fmt.Println("开始转换镜像")
	output := make([]struct {
		Source string
		Target string
		Repository string
	}, 0)

	wg := sync.WaitGroup{}

	for _, source := range content.Content {
		if source == "" {
			continue
		}
		var target string
		// 如果为空,默认推送到 DockerHub 用户名 下
		// 如果指定了值,则推动到指定的仓库下,用户名不一定与repository后缀相同
		if *repository == "" {
// 			target = *username + "/" + strings.ReplaceAll(source, "/", "__")
			target = SanitizeRef(FormatName(*username, source))
		} else {
// 			target = *repository + "/" + strings.ReplaceAll(source, "/", "__")
			target = SanitizeRef(FormatName(*repository, source))
		}

		wg.Add(1)
		go func(source, target, repository string) {
			defer wg.Done()

			fmt.Println("开始转换", source, "=>", target)
			ctx := context.Background()

			// 拉取镜像
			pullOut, err := cli.ImagePull(ctx, source, types.ImagePullOptions{})
			if err != nil {
				panic(err)
			}
			defer pullOut.Close()
			io.Copy(os.Stdout, pullOut)

			// 重新标签
			err = cli.ImageTag(ctx, source, target)
			if err != nil {
				panic(err)
			}

			// 上传镜像
			pushOut, err := cli.ImagePush(ctx, target, types.ImagePushOptions{
				RegistryAuth: authStr,
			})
			if err != nil {
				panic(err)
			}
			defer pushOut.Close()
			io.Copy(os.Stdout, pushOut)

			output = append(output, struct {
				Source string
				Target string
				Repository string
			}{Source: source, Target: target, Repository: repository})
			fmt.Println("转换成功", source, "=>", target)
		}(source, target, *repository)
	}

	wg.Wait()

	if len(output) == 0 {
		panic("output is empty.")
	}

	tmpl, err := template.New("pull_images").Parse(`{{- range . -}}
	
{{if .Repository}}
# if your repository is private,please login...
# docker login {{ .Repository }} --username={your username}
{{end}}	
docker pull {{ .Target }}
docker tag {{ .Target }} {{ .Source }}

{{ end -}}`)
	if err != nil {
		panic(err)
	}
	outputFile, err := os.Create(*outputPath)
	if err != nil {
		panic(err)
	}
	defer outputFile.Close()
	err = tmpl.Execute(outputFile, output)
	if err != nil {
		panic(err)
	}
	fmt.Println(output)
}
