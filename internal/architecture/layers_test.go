package architecture_test

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// 依赖方向是工程约束，直接扫描生产导入，避免搬文件后职责再次回流。
func TestProductionDependencyDirection(t *testing.T) {
	root := filepath.Clean("..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		layer := strings.Split(relative, "/")[0]
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			dependency, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			switch layer {
			case "domain":
				if strings.HasPrefix(dependency, "drama-agent/internal/domain/") {
					continue
				}
				pkg, err := build.Default.Import(dependency, "", build.FindOnly)
				if err != nil || !pkg.Goroot {
					t.Errorf("领域层只能依赖标准库和领域包: %s -> %s", relative, dependency)
				}
			case "application":
				if strings.HasPrefix(filepath.ToSlash(relative), "application/task/") && (dependency == "drama-agent/internal/domain/project" || strings.HasPrefix(dependency, "drama-agent/internal/application/project")) {
					t.Errorf("独立任务不得依赖业务聚合: %s -> %s", relative, dependency)
				}
				if strings.HasPrefix(relative, "application/project/") && dependency == "drama-agent/internal/application/modelconfig" {
					t.Errorf("项目上下文必须通过模型端口访问配置: %s", relative)
				}
				if dependency == "net/http" {
					t.Errorf("应用层不直接做 HTTP 运输: %s -> %s", relative, dependency)
					continue
				}
				if strings.HasPrefix(dependency, "drama-agent/internal/adapter/") || strings.HasPrefix(dependency, "drama-agent/cmd/") {
					t.Errorf("应用层通过端口访问基础设施: %s -> %s", relative, dependency)
					continue
				}
				// 应用层白名单：标准库、内部包，以及唯一许可的第三方包
				// github.com/eino-contrib/jsonschema（工具 schema 由应用层 DTO 生成）。
				// 引入其他第三方依赖前必须先在此说明理由，枚举黑名单拦不住新 SDK。
				if !strings.HasPrefix(dependency, "drama-agent/internal/") && dependency != "github.com/eino-contrib/jsonschema" {
					if pkg, err := build.Default.Import(dependency, "", build.FindOnly); err != nil || !pkg.Goroot {
						t.Errorf("应用层只依赖标准库、内部包与已许可的 schema 反射包: %s -> %s", relative, dependency)
					}
				}
			case "adapter":
				if strings.HasPrefix(dependency, "drama-agent/cmd/") {
					t.Errorf("适配器不能依赖组装入口: %s -> %s", relative, dependency)
				}
				if httpImportsAdapter(relative, dependency) {
					t.Errorf("HTTP 通过应用端口访问其他适配器: %s -> %s", relative, dependency)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func httpImportsAdapter(relative, dependency string) bool {
	return strings.HasPrefix(relative, "adapter/httpapi/") && strings.HasPrefix(dependency, "drama-agent/internal/adapter/")
}

func TestHTTPAdapterBoundaryRecognizesInternalRelativePaths(t *testing.T) {
	for _, sample := range []struct {
		path, dependency string
		violation        bool
	}{
		{"adapter/httpapi/server.go", "drama-agent/internal/adapter/postgres", true},
		{"adapter/httpapi/server.go", "drama-agent/internal/application/accounts", false},
		{"adapter/postgres/accounts.go", "drama-agent/internal/adapter/appconfig", false},
	} {
		if got := httpImportsAdapter(sample.path, sample.dependency); got != sample.violation {
			t.Errorf("%s -> %s: got %t want %t", sample.path, sample.dependency, got, sample.violation)
		}
	}
}

func TestBusinessPackagesAndFileNames(t *testing.T) {
	for _, old := range []string{"catalogapi", "catalogagent", "catalogmodel", "catalogstore", "modelhttp", "source", "application/catalog", "domain/catalog"} {
		if _, err := build.Default.ImportDir(filepath.Join("..", old), build.FindOnly); err == nil {
			t.Errorf("旧技术混合包仍存在: %s", old)
		}
	}
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if len(name) > 2 && name[0] == 'p' && name[1] >= '1' && name[1] <= '9' && (name[2] == '_' || name[2] == '.') {
			t.Errorf("文件以业务职责命名，不能以阶段编号命名: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// 阶段编号属路线文档，不属于源码标识符；文件名检查拦不住 p5Xxx 式函数名，这里按声明再拦一次。
func TestNoStageNumberedIdentifiers(t *testing.T) {
	stageNumbered := regexp.MustCompile(`^[pP][1-9][0-9]*[_A-Z]`)
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		report := func(name string) {
			if stageNumbered.MatchString(name) {
				t.Errorf("标识符以业务职责命名，不能带阶段编号: %s 定义了 %s", path, name)
			}
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				report(d.Name.Name)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						report(s.Name.Name)
					case *ast.ValueSpec:
						for _, id := range s.Names {
							report(id.Name)
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
