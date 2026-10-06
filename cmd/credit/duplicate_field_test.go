package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIDuplicateFieldsRejectReadAndWrite 记录文件任一 JSON 对象内出现
// 重复字段时，无论只读（check/show）还是写入类（登记、成绩提交、免修）
// 命令，都必须沿用记录损坏退出码 2：不输出基于部分数据的结果、不提示成功、
// 不保存任何变更；错误信息点名所读文件与重复字段，原文件字节原样保留
// （其中已有的修读结果与免修依据也不得丢）。
func TestCLIDuplicateFieldsRejectReadAndWrite(t *testing.T) {
	// s1 有一门 4 学分课程并已通过，另有一条带依据的免修历史；课程对象里
	// 写了两个 credit（4 与 9），绝不能按 9 学分继续核对。
	dupCredit := "{\n  \"version\": 1,\n  \"courses\": [\n" +
		"    {\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"credit\": 9," +
		" \"open\": true}\n  ],\n" +
		"  \"students\": [\n    {\"id\": \"s1\"}\n  ],\n" +
		"  \"requirements\": [\n" +
		"    {\"student\": \"s1\", \"id\": \"r1\", \"course\": \"c1\"}\n  ],\n" +
		"  \"enrollments\": [\n" +
		"    {\"student\": \"s1\", \"req\": \"r1\", \"term\": \"2024春\", \"id\": \"e1\"," +
		" \"result\": \"passed\", \"resultSeq\": 1}\n  ],\n" +
		"  \"waivers\": [\n" +
		"    {\"id\": \"wX\", \"student\": \"s1\", \"req\": \"nope\", \"basis\": \"竞赛材料\"," +
		" \"status\": \"rejected\", \"reason\": \"目标要求 nope 不存在或不属于该学生\"}\n" +
		"  ],\n  \"nextResultSeq\": 1\n}\n"

	// 修读记录里重复 student 字段（s1/s2），不能让后者决定归属。
	dupStudent := "{\n  \"version\": 1,\n" +
		"  \"students\": [{\"id\": \"s1\"}, {\"id\": \"s2\"}],\n" +
		"  \"courses\": [{\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"open\": true}],\n" +
		"  \"requirements\": [\n" +
		"    {\"student\": \"s1\", \"id\": \"r1\", \"course\": \"c1\"},\n" +
		"    {\"student\": \"s2\", \"id\": \"r1\", \"course\": \"c1\"}\n  ],\n" +
		"  \"enrollments\": [\n" +
		"    {\"id\": \"e1\", \"student\": \"s1\", \"student\": \"s2\"," +
		" \"req\": \"r1\", \"term\": \"2024春\", \"result\": \"enrolled\"}\n  ],\n" +
		"  \"waivers\": [],\n  \"nextResultSeq\": 0\n}\n"

	// 最外层出现两份 courses，第二份即使是空数组也不能替换第一份。
	dupTop := "{\n  \"version\": 1,\n" +
		"  \"courses\": [{\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"open\": true}],\n" +
		"  \"courses\": [],\n" +
		"  \"students\": [{\"id\": \"s1\"}],\n" +
		"  \"requirements\": [], \"enrollments\": [], \"waivers\": [],\n" +
		"  \"nextResultSeq\": 0\n}\n"

	// 重复字段不相邻、且两个值相同：仍然拒绝。
	dupSame := `{"version":1,"courses":[` +
		`{"credit":4,"id":"c1","name":"数学","open":true,"credit":4}],"students":[]}` + "\n"

	cases := map[string]struct {
		content string
		field   string
	}{
		"课程学分重复":      {dupCredit, "credit"},
		"修读学生编号重复":    {dupStudent, "student"},
		"顶层courses重复": {dupTop, "courses"},
		"同值且不相邻仍重复":   {dupSame, "credit"},
	}

	// 覆盖只读与各类写入入口；任何一个都不得办理或保存。
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
					t.Fatalf("命令 %v 遇重复字段应退出码 %d，code=%d out=%q err=%q",
						args, exitFile, code, out, errText)
				}
				if out != "" {
					t.Fatalf("命令 %v 不应输出任何业务结果，out=%q", args, out)
				}
				if !strings.Contains(errText, "内容损坏") ||
					!strings.Contains(errText, file) ||
					!strings.Contains(errText, tc.field) {
					t.Fatalf("命令 %v 的错误应说明内容损坏、点名文件与重复字段 %q，err=%q",
						args, tc.field, errText)
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

// TestCLIDuplicateFieldDoesNotLeakDataToOthers 损坏文件中即使其他学生的
// 记录本身合法，也不能绕过拒绝：check/show 其他学生同样退出码 2。
func TestCLIDuplicateFieldDoesNotLeakDataToOthers(t *testing.T) {
	content := "{\n  \"version\": 1,\n" +
		"  \"courses\": [{\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"open\": true}],\n" +
		"  \"students\": [{\"id\": \"s1\"}, {\"id\": \"s2\"}],\n" +
		"  \"requirements\": [\n" +
		"    {\"student\": \"s1\", \"id\": \"r1\", \"course\": \"c1\"},\n" +
		"    {\"student\": \"s2\", \"id\": \"r1\", \"course\": \"c1\"}\n  ],\n" +
		"  \"enrollments\": [\n" +
		"    {\"student\": \"s1\", \"id\": \"e1\", \"req\": \"r1\", \"term\": \"2024春\"," +
		" \"result\": \"passed\", \"resultSeq\": 1},\n" +
		"    {\"student\": \"s2\", \"id\": \"e1\", \"student\": \"s2\", \"req\": \"r1\"," +
		" \"term\": \"2024春\", \"result\": \"enrolled\"}\n  ],\n" +
		"  \"waivers\": [],\n  \"nextResultSeq\": 1\n}\n"
	file := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	// s1 的记录本身没有重复键，也不能据此输出正常核对结果。
	for _, student := range []string{"s1", "s2", "ghost"} {
		if out, errText, code := runCLI(t, file, "check", student); code != exitFile {
			t.Fatalf("check %s 应随整份文件拒绝，code=%d out=%q err=%q",
				student, code, out, errText)
		}
		if out, errText, code := runCLI(t, file, "show", student); code != exitFile {
			t.Fatalf("show %s 应随整份文件拒绝，code=%d out=%q err=%q",
				student, code, out, errText)
		}
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatal("拒绝期间原文件必须原样保留")
	}
}

// TestCLICaseVariantFieldsRejectWholeFile 同一 JSON 对象内两个字段名仅大小
// 写不同、却被识别为同一记录字段时（credit/Credit、student/Student、
// courses/COURSES），必须沿用与完全同名字段一致的拒绝：退出码 2，标准错误
// 点名记录文件、说明内容损坏并指出冲突字段名；标准输出不展示核对结果、记录
// 内容或登记成功提示；不挑选其中一个值继续处理，原文件每个字节原样保留。
// 即使冲突只在本次查询未涉及的记录（另一门课程、免修历史）里，也整份拒绝。
func TestCLICaseVariantFieldsRejectWholeFile(t *testing.T) {
	// 课程同时写 credit:4 与 Credit:9：绝不能按书写顺序取 4 或 9。
	caseCredit := "{\n  \"version\": 1,\n  \"courses\": [\n" +
		"    {\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"Credit\": 9," +
		" \"open\": true}\n  ],\n" +
		"  \"students\": [\n    {\"id\": \"s1\"}\n  ],\n" +
		"  \"requirements\": [\n" +
		"    {\"student\": \"s1\", \"id\": \"r1\", \"course\": \"c1\"}\n  ],\n" +
		"  \"enrollments\": [\n" +
		"    {\"student\": \"s1\", \"req\": \"r1\", \"term\": \"2024春\", \"id\": \"e1\"," +
		" \"result\": \"enrolled\"}\n  ],\n" +
		"  \"waivers\": [],\n  \"nextResultSeq\": 0\n}\n"

	// 颠倒书写顺序（先 Credit 后 credit）：结论不能变，错误应指出冲突字段。
	caseCreditReversed := "{\n  \"version\": 1,\n  \"courses\": [\n" +
		"    {\"id\": \"c1\", \"name\": \"数学\", \"Credit\": 9, \"credit\": 4," +
		" \"open\": true}\n  ],\n  \"students\": [],\n  \"requirements\": [],\n" +
		"  \"enrollments\": [],\n  \"waivers\": [],\n  \"nextResultSeq\": 0\n}\n"

	// 修读记录同时有 student 与 Student，不能据此改变所属学生。
	caseStudent := "{\n  \"version\": 1,\n" +
		"  \"students\": [{\"id\": \"s1\"}, {\"id\": \"s2\"}],\n" +
		"  \"courses\": [{\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"open\": true}],\n" +
		"  \"requirements\": [\n" +
		"    {\"student\": \"s1\", \"id\": \"r1\", \"course\": \"c1\"}\n  ],\n" +
		"  \"enrollments\": [\n" +
		"    {\"id\": \"e1\", \"student\": \"s1\", \"Student\": \"s2\"," +
		" \"req\": \"r1\", \"term\": \"2024春\", \"result\": \"enrolled\"}\n  ],\n" +
		"  \"waivers\": [],\n  \"nextResultSeq\": 0\n}\n"

	// 最外层 courses 与 COURSES，第二份即使是空数组也不能盖掉第一份。
	caseTop := "{\n  \"version\": 1,\n" +
		"  \"courses\": [{\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"open\": true}],\n" +
		"  \"COURSES\": [],\n" +
		"  \"students\": [{\"id\": \"s1\"}],\n" +
		"  \"requirements\": [], \"enrollments\": [], \"waivers\": [],\n" +
		"  \"nextResultSeq\": 0\n}\n"

	// 冲突藏在本次核对完全不涉及的一门课程里：仍整份拒绝。
	caseIrrelevant := "{\n  \"version\": 1,\n" +
		"  \"courses\": [\n" +
		"    {\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"open\": true},\n" +
		"    {\"id\": \"c9\", \"name\": \"无关课程\", \"credit\": 3, \"CREDIT\": 3, \"open\": true}\n  ],\n" +
		"  \"students\": [{\"id\": \"s1\"}],\n" +
		"  \"requirements\": [{\"student\": \"s1\", \"id\": \"r1\", \"course\": \"c1\"}],\n" +
		"  \"enrollments\": [],\n  \"waivers\": [],\n  \"nextResultSeq\": 0\n}\n"

	// 用 Unicode 转义写出的大写键与小写直接键冲突：按解码后实际字段判断。
	caseEscaped := "{\n  \"version\": 1,\n  \"courses\": [\n" +
		"    {\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"\\u0043redit\": 9," +
		" \"open\": true}\n  ],\n  \"students\": [],\n  \"requirements\": [],\n" +
		"  \"enrollments\": [],\n  \"waivers\": [],\n  \"nextResultSeq\": 0\n}\n"

	cases := map[string]struct {
		content string
		field   string
	}{
		"学分大小写冲突":     {caseCredit, "credit"},
		"学分大小写冲突顺序颠倒": {caseCreditReversed, "Credit"},
		"修读学生字段大小写冲突": {caseStudent, "student"},
		"顶层数组字段大小写冲突": {caseTop, "courses"},
		"冲突在无关课程中仍拒绝": {caseIrrelevant, "credit"},
		"转义大写键与小写键冲突": {caseEscaped, "credit"},
	}

	commands := [][]string{
		{"check", "s1"},
		{"show", "s1"},
		{"list-courses"},
		{"student", "s9"},
		{"course", "c9", "物理", "3"},
		{"req", "s1", "r9", "c1"},
		{"enroll", "s1", "r1", "2025春", "e9"},
		{"pass", "s1", "e1"},
		{"waiver", "s1", "r1", "w9", "新依据"},
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
					t.Fatalf("命令 %v 遇大小写冲突字段应退出码 %d，code=%d out=%q err=%q",
						args, exitFile, code, out, errText)
				}
				if out != "" {
					t.Fatalf("命令 %v 不应输出任何核对结果或成功提示，out=%q", args, out)
				}
				if !strings.Contains(errText, "内容损坏") ||
					!strings.Contains(errText, file) ||
					!strings.Contains(errText, tc.field) {
					t.Fatalf("命令 %v 的错误应说明内容损坏、点名文件并指出冲突字段 %q，err=%q",
						args, tc.field, errText)
				}
				got, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != tc.content {
					t.Fatalf("命令 %v 不得挑选值或改动原文件\nwant=%q\n got=%q",
						args, tc.content, got)
				}
			}
		})
	}
}

// TestCLISingleCaseVariantFieldStillReads 兼容性：字段只出现一次时，大写
// 写法仍能被识别读取（只有 Credit 的课程按该值读取），核对照常输出。
func TestCLISingleCaseVariantFieldStillReads(t *testing.T) {
	content := "{\n  \"version\": 1,\n" +
		"  \"courses\": [{\"id\": \"c1\", \"name\": \"数学\", \"Credit\": 4, \"Open\": true}],\n" +
		"  \"students\": [{\"id\": \"s1\"}],\n" +
		"  \"requirements\": [{\"id\": \"r1\", \"Student\": \"s1\", \"Course\": \"c1\"}],\n" +
		"  \"enrollments\": [\n" +
		"    {\"id\": \"e1\", \"student\": \"s1\", \"req\": \"r1\", \"term\": \"2024春\",\n" +
		"     \"result\": \"passed\", \"resultSeq\": 1}],\n" +
		"  \"waivers\": [],\n  \"nextResultSeq\": 1\n}\n"
	file := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errText, code := runCLI(t, file, "check", "s1")
	if code != exitOK || !strings.Contains(out, "总学分：4") {
		t.Fatalf("单个 Credit 大写字段应照常按 4 学分读取，code=%d out=%q err=%q",
			code, out, errText)
	}
	if out, _, code := runCLI(t, file, "list-courses"); code != exitOK ||
		!strings.Contains(out, "4 学分") || !strings.Contains(out, "开放") {
		t.Fatalf("大写 Credit/Open 字段应照常列出，code=%d out=%q", code, out)
	}
}

// 两名学生各自的同号要求、修读与免修保持现有归属规则；课程名称/免修依据
// 文字里提到字段名也不得被误判为重复。
func TestCLISameFieldAcrossDifferentObjectsStillWorks(t *testing.T) {
	// 两名学生各自有同号要求 r1、同号修读 e1：重复的是“不同对象内的同名字段”，
	// 不是同一对象内的重复键，成绩提交仍各归各。
	file := filepath.Join(t.TempDir(), "records.json")
	setupCLISharedEnrollments(t, file)
	if _, _, code := runCLI(t, file, "pass", "s1", "e1"); code != 0 {
		t.Fatal("s1 提交通过应正常")
	}
	if out, _, code := runCLI(t, file, "check", "s1"); code != 0 ||
		!strings.Contains(out, "总学分：4") {
		t.Fatalf("s1 应按本人修读计 4 学分，code=%d out=%q", code, out)
	}
	if out, _, code := runCLI(t, file, "check", "s2"); code != 0 ||
		!strings.Contains(out, "总学分：0") {
		t.Fatalf("s2 不受 s1 同号字段影响，仍为 0 学分，code=%d out=%q", code, out)
	}

	// 另一份文件：字段名出现在课程名称、免修依据等字符串值中，读取不应误判。
	textFile := filepath.Join(t.TempDir(), "records.json")
	content := "{\n  \"version\": 1,\n" +
		"  \"courses\": [{\"id\": \"c1\", \"name\": \"本课程名称里提到 credit 字段\"," +
		" \"credit\": 4, \"open\": true}],\n" +
		"  \"students\": [{\"id\": \"s1\"}],\n" +
		"  \"requirements\": [{\"id\": \"r1\", \"student\": \"s1\", \"course\": \"c1\"}],\n" +
		"  \"enrollments\": [],\n" +
		"  \"waivers\": [{\"id\": \"w1\", \"student\": \"s1\", \"req\": \"rx\"," +
		" \"basis\": \"依据文字中提到 student 字段名\", \"status\": \"rejected\"," +
		" \"reason\": \"目标要求 rx 不存在或不属于该学生\"}],\n" +
		"  \"nextResultSeq\": 0\n}\n"
	if err := os.WriteFile(textFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, code := runCLI(t, textFile, "show", "s1")
	if code != 0 || !strings.Contains(out, "提到 credit 字段") ||
		!strings.Contains(out, "提到 student 字段名") {
		t.Fatalf("字符串内容里出现字段名应照常读取，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, textFile, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "w1") {
		t.Fatalf("合法记录的核对与免修历史规则应保持，code=%d out=%q", code, out)
	}
}
