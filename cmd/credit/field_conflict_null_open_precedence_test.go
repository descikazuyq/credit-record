package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件从命令行入口回归字段冲突与课程开放状态为空两类损坏并存时的诊断
// 类别：即使 open:null 课程排在文件最前，读取也必须按字段冲突以退出码 2
// 结束——错误类别不因问题课程的排列位置改变；stdout 无任何登记/核对输出，
// 原文件逐字节保留。

// TestCLIFieldConflictTakesPrecedenceOverNullOpen 顶层 courses 数组中第一
// 门课 open:null，第二门课同时写 credit 与 Credit：空状态课程排得更靠前，
// 但任何子命令访问都应报告字段归属冲突而不是“开放状态为空”。
func TestCLIFieldConflictTakesPrecedenceOverNullOpen(t *testing.T) {
	content := "{\n" +
		`  "version": 1,` + "\n" +
		`  "courses": [` + "\n" +
		`    {"id": "c1", "name": "数学", "credit": 4, "open": null},` + "\n" +
		`    {"id": "c2", "name": "物理", "credit": 3, "Credit": 8, "open": true}` + "\n" +
		`  ],` + "\n" +
		`  "students": [{"id": "s1"}]` + "\n" +
		"}\n"
	for _, args := range [][]string{
		{"list-courses"},
		{"check", "s1"},
		{"student", "s2"},
		{"course-open", "c1"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			out, errText, code := runCLI(t, file, args...)
			if code != exitFile || out != "" {
				t.Fatalf("命令 %v 应退出码 %d 且无 stdout，code=%d out=%q err=%q",
					args, exitFile, code, out, errText)
			}
			if !strings.Contains(errText, "字段归属无法确定") ||
				!strings.Contains(errText, "credit") ||
				!strings.Contains(errText, "Credit") {
				t.Fatalf("应优先报告字段冲突，err=%q", errText)
			}
			if strings.Contains(errText, "开放状态为空") {
				t.Fatalf("字段冲突优先，不应出现空状态诊断，err=%q", errText)
			}
			assertFileByteIdentical(t, file, []byte(content), "拒绝之后：")
		})
	}
}
