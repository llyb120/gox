package compiler

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/llyb120/gox/parser"
)

type Compiler struct {
	Incremental     bool   //是否增量编译
	SingleFile      string // 是否只编译一个文件
	DebugMode       bool   // 开启调试模式
	RemoveGenerated string // 移除生成的文件目录

	SrcPath  string // 源文件路径
	DestPath string // 目标文件路径
}

func (c *Compiler) Compile() {
	// 添加增量编译参数
	var incremental = c.Incremental
	var singleFile = c.SingleFile
	var debugMode = c.DebugMode
	var removeGenerated = c.RemoveGenerated
	// flag.BoolVar(&incremental, "incremental", false, "启用增量编译，跳过已经是最新的文件")
	// flag.BoolVar(&incremental, "i", false, "启用增量编译的简写形式")
	// flag.StringVar(&singleFile, "f", "", "单独编译一个文件")
	// flag.BoolVar(&debugMode, "debug", false, "启用调试模式，显示详细的错误信息和预处理后的代码")
	// flag.BoolVar(&debugMode, "d", false, "启用调试模式的简写形式")
	// flag.StringVar(&removeGenerated, "r", "", "移除生成的文件目录")
	// flag.Parse()

	if singleFile != "" {
		if err := c.processGoxFile(singleFile, incremental, debugMode); err != nil {
			log.Fatal(err)
		}
		return
	}

	// 移除简单的 sleep，使用更可靠的文件检查机制
	// time.Sleep(100 * time.Millisecond)

	// 获取工作目录
	// path, err := os.Getwd()
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// path := filepath.Join(cwd, "../")
	// 换算绝对路径
	path := c.SrcPath
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		path = filepath.Join(cwd, path)
	}

	// 如果根目录是cmd，取父级目录
	// if filepath.Base(path) == "cmd" {
	// 	path = filepath.Dir(path)
	// }

	// 检查是文件还是目录
	info, err := os.Stat(path)
	if err != nil {
		log.Fatal(err)
	}

	if removeGenerated != "" {
		// 移除该目录下所有_gen.go文件
		_ = filepath.Walk(removeGenerated, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, "_gen.go") {
				fmt.Printf("移除文件：%s\n", path)
				err = os.Remove(path)
				if err != nil {
					return err
				}
			}
			return nil
		})
	}

	if info.IsDir() {
		if err := c.processDirectory(path, incremental, debugMode); err != nil {
			log.Fatal(err)
		}
	} else {
		if err := c.processGoxFile(path, incremental, debugMode); err != nil {
			log.Fatal(err)
		}
	}
}

func (c *Compiler) processDirectory(dir string, incremental bool, debugMode bool) error {
	fmt.Printf("处理目录: %s\n", dir)

	var wg sync.WaitGroup
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if d.IsDir() {
			return nil
		}

		// 只处理 .gox.go 文件
		if !strings.HasSuffix(path, ".gox.go") {
			return nil
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			err = c.processGoxFile(path, incremental, debugMode)
			if err != nil {
				panic(err)
			}
		}()
		return nil
	})

	wg.Wait()
	return nil
}

func (c *Compiler) processGoxFile(goxPath string, incremental bool, debugMode bool) error {
	fmt.Printf("处理文件: %s\n", goxPath)

	// 生成目标文件路径
	goPath := strings.TrimSuffix(goxPath, ".gox.go") + "_gen.go"

	// 增量编译检查
	if incremental {
		if shouldSkip, err := shouldSkipFile(goxPath, goPath); err != nil {
			fmt.Printf("检查文件时间时出错 %s: %v\n", goxPath, err)
		} else if shouldSkip {
			fmt.Printf("跳过文件（目标文件已是最新）: %s\n", goxPath)
			return nil
		}
	}

	// 读取源文件
	content, err := os.ReadFile(goxPath)
	if err != nil {
		return fmt.Errorf("读取文件失败 %s: %v", goxPath, err)
	}

	// 直接重写目标文件，无需先删除

	// 给源文件添加编译忽略指令
	//if err := addBuildIgnore(goxPath, string(content)); err != nil {
	//	return fmt.Errorf("添加编译忽略指令失败: %v", err)
	//}

	// 解析并生成目标文件
	p := parser.NewParser()
	p.SetDebugMode(debugMode) // 设置调试模式
	goxFile, err := p.ParseFile(goxPath, content)
	if err != nil {
		return fmt.Errorf("解析文件失败: %v", err)
	}

	// 生成Go代码
	generator := parser.NewGenerator()
	generated, err := generator.GenerateFile(goxFile)
	if err != nil {
		return fmt.Errorf("生成代码失败: %v", err)
	}

	// goPath = strings.Replace(goPath, "v3_source", "v3", 1)
	goPath = c.DestPath
	if !filepath.IsAbs(goPath) {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		goPath = filepath.Join(cwd, goPath)
	}

	// 写入目标文件
	if err := os.WriteFile(goPath, generated, 0644); err != nil {
		return fmt.Errorf("写入文件失败 %s: %v", goPath, err)
	}

	fmt.Printf("生成文件: %s\n", goPath)
	return nil
}

// shouldSkipFile 检查是否应该跳过文件编译
// 如果目标文件存在且修改时间大于等于源文件，则返回true
func shouldSkipFile(srcPath, destPath string) (bool, error) {
	// 获取源文件信息
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return false, fmt.Errorf("获取源文件信息失败: %v", err)
	}

	// 获取目标文件信息
	destInfo, err := os.Stat(destPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 目标文件不存在，需要编译
			return false, nil
		}
		return false, fmt.Errorf("获取目标文件信息失败: %v", err)
	}

	// 如果目标文件的修改时间大于等于源文件，则跳过
	return destInfo.ModTime().Compare(srcInfo.ModTime()) >= 0, nil
}

func addBuildIgnore(filePath, content string) error {
	lines := strings.Split(content, "\n")

	// 检查是否已经有编译忽略指令
	hasIgnore := false
	packageIndex := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//go:build ignore") ||
			strings.HasPrefix(trimmed, "// +build ignore") {
			hasIgnore = true
		}
		if strings.HasPrefix(trimmed, "package ") {
			packageIndex = i
			break
		}
	}

	// 如果已经有忽略指令，不需要添加
	if hasIgnore {
		return nil
	}

	// 在package行前添加编译忽略指令
	if packageIndex == -1 {
		return fmt.Errorf("找不到package声明")
	}

	newLines := make([]string, 0, len(lines)+3)

	// 添加到package行之前
	newLines = append(newLines, lines[:packageIndex]...)
	newLines = append(newLines, "//go:build ignore")
	newLines = append(newLines, "// +build ignore")
	newLines = append(newLines, "")
	newLines = append(newLines, lines[packageIndex:]...)

	newContent := strings.Join(newLines, "\n")
	return os.WriteFile(filePath, []byte(newContent), 0644)
}
