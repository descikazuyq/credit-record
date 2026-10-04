package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 一份可正常加载的最小记录：学生 s1 与 4 学分课程 c1。
const existingRecordsJSON = `{
  "version": 1,
  "courses": [
    {"id": "c1", "name": "数学", "credit": 4, "open": true}
  ],
  "students": [
    {"id": "s1"}
  ],
  "requirements": null,
  "enrollments": null,
  "waivers": null,
  "nextResultSeq": 0
}
`

// TestCLISaveFailureNewFileHidesBusinessResult 记录文件与父目录都不存在时，
// 保存必然失败。此时命令必须：退出码 2；标准输出没有任何业务结果，也没有
// “已从空记录开始并创建”的提示；标准错误明确指出保存失败并点名目标文件；
// 目标文件与其父目录都不得被创建。首次登记与首次更新都遵守同一规则。
func TestCLISaveFailureNewFileHidesBusinessResult(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		business string // 成功时本应出现在标准输出的关键业务字样
	}{
		{"登记学生", []string{"student", "s1"}, "已登记学生"},
		{"登记课程", []string{"course", "c1", "数学", "4"}, "已登记课程"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			missing := filepath.Join(t.TempDir(), "no", "such", "dir", "records.json")
			out, errText, code := runCLI(t, missing, tc.args...)
			if code != exitFile {
				t.Fatalf("保存失败应退出码 %d，code=%d out=%q err=%q",
					exitFile, code, out, errText)
			}
			if out != "" {
				t.Fatalf("保存失败时标准输出不应包含业务结果，out=%q", out)
			}
			if strings.Contains(out, tc.business) {
				t.Fatalf("不得把尚未写入文件的变更说成已完成，out=%q", out)
			}
			if strings.Contains(out, "从空记录开始") || strings.Contains(out, "创建") {
				t.Fatalf("文件未创建时不得出现创建提示，out=%q", out)
			}
			if !strings.Contains(errText, "保存") || !strings.Contains(errText, missing) {
				t.Fatalf("标准错误应指出保存失败并点名目标记录文件，err=%q", errText)
			}
			if _, err := os.Stat(missing); !os.IsNotExist(err) {
				t.Fatalf("保存失败后记录文件不应存在，stat err=%v", err)
			}
			if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
				t.Fatalf("不得创建缺失的父目录，stat err=%v", err)
			}
		})
	}
}

// makeReadOnlyDir 在临时目录下放入一份现有记录文件，并把目录改为只读使保存
// 阶段无法创建临时文件。返回记录文件路径；测试结束自动恢复权限以便清理。
// root 身份绕过目录权限，无法用这种方式诱发保存失败，因此跳过。
func makeReadOnlyDir(t *testing.T, content string) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root 绕过只读目录权限，无法诱发保存失败")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	return file
}

// TestCLISaveFailureExistingFilePreservesContent 修改已有记录时保存失败：
// 退出码 2，标准输出没有登记/更新结果，标准错误点名保存失败与目标文件，
// 原文件每个字节保留。
func TestCLISaveFailureExistingFilePreservesContent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		business string
	}{
		{"新增学生", []string{"student", "s9"}, "已登记学生"},
		{"更新未被引用课程", []string{"course", "c1", "物理", "3"}, "已更新"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := makeReadOnlyDir(t, existingRecordsJSON)
			out, errText, code := runCLI(t, file, tc.args...)
			if code != exitFile {
				t.Fatalf("保存失败应退出码 %d，code=%d out=%q err=%q",
					exitFile, code, out, errText)
			}
			if out != "" {
				t.Fatalf("保存失败时标准输出不应有业务结果，out=%q", out)
			}
			if strings.Contains(out, tc.business) {
				t.Fatalf("不得把未保存的变更说成已完成，out=%q", out)
			}
			if !strings.Contains(errText, "保存") || !strings.Contains(errText, file) {
				t.Fatalf("标准错误应指出保存失败并点名目标记录文件，err=%q", errText)
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != existingRecordsJSON {
				t.Fatalf("已有记录文件必须原样保留\nwant=%q\n got=%q",
					existingRecordsJSON, got)
			}
		})
	}
}

// TestCLISaveFailureRejectedWaiverHidesHistory 因目标要求不存在而被拒绝的
// 首次免修申请需要把申请与原因写入历史，因此仍要保存。保存失败时必须以
// 文件错误退出码 2 结束，不能显示“已拒绝/已记入历史”，业务拒绝（退出码 1）
// 不能掩盖保存失败；原文件保留。有效免修同理：保存失败时不得显示有效与
// 获得学分。
func TestCLISaveFailureRejectedWaiverHidesHistory(t *testing.T) {
	t.Run("被拒绝的申请", func(t *testing.T) {
		file := makeReadOnlyDir(t, existingRecordsJSON)
		out, errText, code := runCLI(t, file, "waiver", "s1", "rX", "wbad", "竞赛获奖")
		if code != exitFile {
			t.Fatalf("历史未保存成功应退出码 %d，code=%d out=%q err=%q",
				exitFile, code, out, errText)
		}
		if out != "" {
			t.Fatalf("保存失败时标准输出不应有业务结果，out=%q", out)
		}
		if strings.Contains(out, "已拒绝") || strings.Contains(out, "已记入") {
			t.Fatalf("历史未保存不得宣称申请已拒绝且已记入历史，out=%q", out)
		}
		if !strings.Contains(errText, "保存") || !strings.Contains(errText, file) {
			t.Fatalf("应报告文件错误并点名记录文件，err=%q", errText)
		}
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != existingRecordsJSON {
			t.Fatalf("保存失败后原文件必须保留，got=%q", got)
		}
	})

	t.Run("有效的申请", func(t *testing.T) {
		withReq := strings.Replace(existingRecordsJSON,
			`"requirements": null`,
			`"requirements": [
    {"student": "s1", "id": "r1", "course": "c1"}
  ]`, 1)
		file := makeReadOnlyDir(t, withReq)
		out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "竞赛获奖")
		if code != exitFile {
			t.Fatalf("保存失败应退出码 %d，code=%d out=%q err=%q",
				exitFile, code, out, errText)
		}
		if out != "" {
			t.Fatalf("保存失败时标准输出不应有业务结果，out=%q", out)
		}
		if strings.Contains(out, "有效") || strings.Contains(out, "获得课程学分") {
			t.Fatalf("只有保存成功后才能显示免修有效及获得学分，out=%q", out)
		}
		if !strings.Contains(errText, "保存") {
			t.Fatalf("应报告保存失败，err=%q", errText)
		}
	})
}

// TestCLISuccessfulCreateOrdersBusinessBeforeNote 保存成功时业务输出保持
// 原有内容与先后顺序，首次创建记录文件的说明位于业务结果之后。
func TestCLISuccessfulCreateOrdersBusinessBeforeNote(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	out, _, code := runCLI(t, file, "student", "s1")
	if code != 0 {
		t.Fatalf("正常登记应退出码 0，code=%d out=%q", code, out)
	}
	business := strings.Index(out, "已登记学生 s1")
	note := strings.Index(out, "从空记录开始")
	if business < 0 || note < 0 {
		t.Fatalf("应同时包含业务结果与首次创建说明，out=%q", out)
	}
	if business > note {
		t.Fatalf("首次创建说明应放在业务结果之后，out=%q", out)
	}
}
