package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// escapedCreditKey 是 JSON 源文本中的转义写法，解码后为 "credit"。
const escapedCreditKey = "\"\\u0063redit\""

// TestCLICaseInsensitiveFieldConflictsRejectReadAndWrite 记录文件任一 JSON
// 对象内出现两个仅大小写不同、却指向同一记录字段的键时，无论只读还是
// 写入类命令都必须沿用记录损坏退出码 2：不输出基于部分数据的结果、不
// 提示成功、不保存任何变更；标准错误点名记录文件、说明内容损坏并指出
// 冲突的两个字段名；原文件字节原样保留。字段顺序、是否相邻、两个值
// 是否相同都不影响拒绝结果。
func TestCLICaseInsensitiveFieldConflictsRejectReadAndWrite(t *testing.T) {
	// credit=4 与 Credit=9：绝不能按后一个值核对。
	dupCredit := "{\n  \"version\": 1,\n  \"courses\": [\n" +
		"    {\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"Credit\": 9," +
		" \"open\": true}\n  ],\n" +
		"  \"students\": [\n    {\"id\": \"s1\"}\n  ],\n" +
		"  \"requirements\": [\n" +
		"    {\"student\": \"s1\", \"id\": \"r1\", \"course\": \"c1\"}\n  ],\n" +
		"  \"enrollments\": [\n" +
		"    {\"student\": \"s1\", \"req\": \"r1\", \"term\": \"2024春\", \"id\": \"e1\"," +
		" \"result\": \"passed\", \"resultSeq\": 1}\n  ],\n" +
		"  \"waivers\": [],\n  \"nextResultSeq\": 1\n}\n"

	// 顺序颠倒：Credit=4 在前、credit=9 在后，仍必须拒绝，不能读出 9。
	dupCreditReversed := "{\n  \"version\": 1,\n  \"courses\": [\n" +
		"    {\"Credit\": 4, \"id\": \"c1\", \"name\": \"数学\"," +
		" \"open\": true, \"credit\": 9}\n  ],\n  \"students\": [],\n" +
		"  \"requirements\": [], \"enrollments\": [], \"waivers\": [],\n" +
		"  \"nextResultSeq\": 0\n}\n"

	// 修读里同时有 student 和 Student，不能据此改变所属学生。
	dupStudent := "{\n  \"version\": 1,\n" +
		"  \"students\": [{\"id\": \"s1\"}, {\"id\": \"s2\"}],\n" +
		"  \"courses\": [{\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"open\": true}],\n" +
		"  \"enrollments\": [\n" +
		"    {\"id\": \"e1\", \"student\": \"s1\", \"Student\": \"s2\"," +
		" \"req\": \"r1\", \"term\": \"2024春\", \"result\": \"enrolled\"}\n  ],\n" +
		"  \"waivers\": [],\n  \"nextResultSeq\": 0\n}\n"

	// 最外层 courses 与 COURSES 同时出现，后一份即使是空数组也不能盖掉前一份。
	dupTop := "{\n  \"version\": 1,\n" +
		"  \"courses\": [{\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"open\": true}],\n" +
		"  \"COURSES\": [],\n" +
		"  \"students\": [{\"id\": \"s1\"}],\n" +
		"  \"requirements\": [], \"enrollments\": [], \"waivers\": [],\n" +
		"  \"nextResultSeq\": 0\n}\n"

	// Unicode 转义（解码后为 credit）与直接书写的 Credit 组合也不能绕开。
	dupEscaped := `{"version":1,"courses":[` +
		`{"id":"c1","name":"数学",` + escapedCreditKey + `:4,"Credit":9,"open":true}],` +
		`"students":[]}` + "\n"

	// 冲突在本次核对没有涉及的免修历史里，仍整份拒绝。
	dupInIrrelevantWaiver := "{\n  \"version\": 1,\n" +
		"  \"courses\": [{\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"open\": true}],\n" +
		"  \"students\": [{\"id\": \"s1\"}],\n" +
		"  \"requirements\": [{\"id\": \"r1\", \"student\": \"s1\", \"course\": \"c1\"}],\n" +
		"  \"enrollments\": [{\"id\": \"e1\", \"student\": \"s1\", \"req\": \"r1\"," +
		" \"term\": \"2024春\", \"result\": \"passed\", \"resultSeq\": 1}],\n" +
		"  \"waivers\": [\n" +
		"    {\"id\": \"wX\", \"student\": \"s1\", \"Student\": \"s1\", \"req\": \"rX\"," +
		" \"basis\": \"历史材料\", \"status\": \"rejected\"," +
		" \"reason\": \"目标要求 rX 不存在或不属于该学生\"}\n  ],\n" +
		"  \"nextResultSeq\": 1\n}\n"

	cases := map[string]struct {
		content      string
		field        string
		otherVariant string
	}{
		"课程credit与Credit冲突":   {dupCredit, "credit", "Credit"},
		"顺序颠倒仍冲突":             {dupCreditReversed, "Credit", "credit"},
		"修读student与Student冲突": {dupStudent, "student", "Student"},
		"顶层courses与COURSES冲突": {dupTop, "courses", "COURSES"},
		"转义加大小写组合冲突":          {dupEscaped, "credit", "Credit"},
		"无关免修历史里的冲突仍整份拒绝":     {dupInIrrelevantWaiver, "student", "Student"},
	}

	commands := [][]string{
		{"check", "s1"},
		{"show", "s1"},
		{"student", "s9"},
		{"course", "c9", "物理", "3"},
		{"req", "s1", "r9", "c1"},
		{"enroll", "s1", "r1", "2025春", "e9"},
		{"pass", "s1", "e1"},
		{"fail", "s1", "e1"},
		{"waiver", "s1", "r1", "w9", "新依据"},
		{"revoke-waiver", "s1", "w9", "撤销原因"},
		{"list-courses"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for _, args := range commands {
				file := filepath.Join(t.TempDir(), "records.json")
				if err := os.WriteFile(file, []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
				out, errText, code := runCLI(t, file, args...)
				if code != exitFile {
					t.Fatalf("命令 %v 遇大小写字段冲突应退出码 %d，code=%d out=%q err=%q",
						args, exitFile, code, out, errText)
				}
				if out != "" {
					t.Fatalf("命令 %v 不应输出任何业务结果，out=%q", args, out)
				}
				if !strings.Contains(errText, "内容损坏") ||
					!strings.Contains(errText, file) ||
					!strings.Contains(errText, tc.field) ||
					!strings.Contains(errText, tc.otherVariant) {
					t.Fatalf("命令 %v 的错误应说明内容损坏、点名文件与冲突字段 %q/%q，err=%q",
						args, tc.field, tc.otherVariant, errText)
				}
				got, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != tc.content {
					t.Fatalf("命令 %v 不得改动原文件\nwant=%q\n got=%q",
						args, tc.content, got)
				}
			}
		})
	}
}

// TestCLICaseVariantOnceStillReadable 兼容性：每个可识别字段在同一对象内
// 只出现一次时，现有读取功能能够识别的大小写写法仍然有效——只有 Credit
// 的课程按该值读取；字段值中的大小写按原文保留，学生 s1 与 S1 是两名
// 不同学生；依据文字里出现 credit 一词不是重复字段。
func TestCLICaseVariantOnceStillReadable(t *testing.T) {
	content := "{\n  \"VERSION\": 1,\n" +
		"  \"COURSES\": [{\"id\": \"c1\", \"name\": \"credit 一词出现在名称里\"," +
		" \"Credit\": 9, \"OPEN\": true}],\n" +
		"  \"STUDENTS\": [{\"id\": \"s1\"}, {\"id\": \"S1\"}],\n" +
		"  \"REQUIREMENTS\": [\n" +
		"    {\"ID\": \"r1\", \"STUDENT\": \"s1\", \"COURSE\": \"c1\"},\n" +
		"    {\"ID\": \"r1\", \"Student\": \"S1\", \"Course\": \"c1\"}\n  ],\n" +
		"  \"ENROLLMENTS\": [\n" +
		"    {\"ID\": \"e1\", \"STUDENT\": \"s1\", \"REQ\": \"r1\", \"TERM\": \"2024春\"," +
		" \"RESULT\": \"passed\", \"RESULTSEQ\": 1}\n  ],\n" +
		"  \"WAIVERS\": [],\n  \"NEXTRESULTSEQ\": 1\n}\n"
	file := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// 只有 Credit=9 的课程按 9 学分读取，课程名称里的 credit 一词照常展示。
	out, _, code := runCLI(t, file, "list-courses")
	if code != 0 || !strings.Contains(out, "9 学分") ||
		!strings.Contains(out, "credit 一词出现在名称里") {
		t.Fatalf("单次出现的 Credit 应按该值读取，code=%d out=%q", code, out)
	}

	// s1 凭本人通过修读得 9 学分；S1 是另一名学生，要求未满足、0 学分。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：9") ||
		!strings.Contains(out, "通过修读 e1") {
		t.Fatalf("s1 应按本人修读得 9 学分，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "S1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("S1 应是 0 学分且要求未满足的独立学生，code=%d out=%q", code, out)
	}

	// show S1 只列出 S1 名下那份要求，不得串到 s1 的修读。
	out, _, code = runCLI(t, file, "show", "S1")
	if code != 0 || !strings.Contains(out, "要求 r1") || strings.Contains(out, "修读 e1") {
		t.Fatalf("S1 名下只应有要求、不应借用 s1 的修读，code=%d out=%q", code, out)
	}

	// 读入后继续办理与保存仍正常：给 S1 的要求补一条修读并核对。
	if _, _, code := runCLI(t, file, "enroll", "S1", "r1", "2024春", "e1"); code != 0 {
		t.Fatal("大小写写法文件读入后应能继续登记")
	}
	if _, _, code := runCLI(t, file, "pass", "S1", "e1"); code != 0 {
		t.Fatal("S1 提交通过应正常")
	}
	out, _, code = runCLI(t, file, "check", "S1")
	if code != 0 || !strings.Contains(out, "总学分：9") {
		t.Fatalf("S1 通过后应得 9 学分，code=%d out=%q", code, out)
	}
	// s1 不受影响，仍是自己的 9 学分与同号修读 e1。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：9") ||
		!strings.Contains(out, "通过修读 e1") {
		t.Fatalf("s1 结果应保持不变，code=%d out=%q", code, out)
	}
}
