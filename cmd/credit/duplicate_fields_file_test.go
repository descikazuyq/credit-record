package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIDuplicateObjectFieldsRejected 同一 JSON 对象内字段重复时，只读与
// 写入类命令都必须以记录文件损坏（退出码 2）结束：check/show 不能给出基于
// 其中一部分数据的结果，登记、成绩提交、免修不能提示成功或保存任何变更，
// 错误输出点名文件与重复字段，原文件字节（含已有修读结果与免修依据）原样
// 保留。文件内其他学生的记录即使合法也不能绕过拒绝。
func TestCLIDuplicateObjectFieldsRejected(t *testing.T) {
	// 除重复字段外完全合法的一份记录：s1 的课程 4 学分且已通过，
	// 另有一条有效免修，用来验证拒绝时这些既有内容必须原样保留。
	base := func(courseObj string) string {
		return "{\n  \"version\": 1,\n  \"courses\": [\n    " + courseObj + "\n  ],\n" +
			"  \"students\": [\n    {\"id\": \"s1\"}\n  ],\n" +
			"  \"requirements\": [\n" +
			"    {\"student\": \"s1\", \"id\": \"r1\", \"course\": \"c1\"}\n  ],\n" +
			"  \"enrollments\": [\n" +
			"    {\"student\": \"s1\", \"req\": \"r1\", \"term\": \"2024春\", \"id\": \"e1\"," +
			" \"result\": \"passed\", \"resultSeq\": 1}\n  ],\n" +
			"  \"waivers\": [],\n  \"nextResultSeq\": 1\n}\n"
	}

	cases := map[string]struct {
		content   string
		fieldName string
	}{
		"课程credit重复(4和9)": {
			base(`{"id": "c1", "name": "数学", "credit": 4, "open": true, "credit": 9}`),
			"credit",
		},
		"顶层两份courses第二份为空数组": {
			"{\n  \"version\": 1,\n  \"courses\": [],\n  \"courses\": []\n}\n",
			"courses",
		},
		"修读student重复指向后一个编号": {
			"{\n  \"version\": 1,\n  \"students\": [{\"id\": \"s1\"}, {\"id\": \"s2\"}],\n" +
				"  \"enrollments\": [{\"student\": \"s2\", \"student\": \"s9\"," +
				" \"id\": \"eX\", \"req\": \"rX\", \"term\": \"2024春\", \"result\": \"enrolled\"}]\n}\n",
			"student",
		},
		"Unicode转义键与直写键同名": {
			"{\"version\":1,\"courses\":[{\"id\":\"c1\",\"name\":\"数学\"," +
				"\"credit\":4,\"\\u0063redit\":9,\"open\":true}]}\n",
			"credit",
		},
	}

	readCmds := [][]string{
		{"check", "s1"},
		{"show", "s1"},
	}
	writeCmds := [][]string{
		{"student", "s9"},
		{"course", "c9", "物理", "3"},
		{"req", "s1", "r9", "c1"},
		{"enroll", "s1", "r1", "2024秋", "e9"},
		{"pass", "s1", "e1"},
		{"waiver", "s1", "r1", "w9", "竞赛获奖"},
		{"revoke-waiver", "s1", "w9"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for _, cmdArgs := range append(readCmds, writeCmds...) {
				file := filepath.Join(t.TempDir(), "records.json")
				if err := os.WriteFile(file, []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
				out, errText, code := runCLI(t, file, cmdArgs...)
				if code != exitFile {
					t.Fatalf("%s/%v：应退出码 %d，code=%d out=%q err=%q",
						name, cmdArgs, exitFile, code, out, errText)
				}
				if out != "" {
					t.Fatalf("%s/%v：拒绝读取时不应有任何业务输出，out=%q",
						name, cmdArgs, out)
				}
				if !strings.Contains(errText, "内容损坏") ||
					!strings.Contains(errText, file) ||
					!strings.Contains(errText, tc.fieldName) {
					t.Fatalf("%s/%v：错误输出应说明内容损坏并点名文件与重复字段 %q，err=%q",
						name, cmdArgs, tc.fieldName, errText)
				}
				got, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != tc.content {
					t.Fatalf("%s/%v：原文件必须原样保留\nwant=%q\n got=%q",
						name, cmdArgs, tc.content, got)
				}
			}
		})
	}
}

// TestCLIDuplicateFieldDifferentObjectsAllowed 不同课程各有 credit、不同
// 修读各有 student 属于正常记录，check/show 等命令照常工作。
func TestCLIDuplicateFieldDifferentObjectsAllowed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	content := "{\n  \"version\": 1,\n  \"courses\": [\n" +
		"    {\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"open\": true},\n" +
		"    {\"id\": \"c2\", \"name\": \"物理\", \"credit\": 3, \"open\": true}\n" +
		"  ],\n  \"students\": [{\"id\": \"s1\"}],\n" +
		"  \"requirements\": [\n" +
		"    {\"student\": \"s1\", \"id\": \"r1\", \"course\": \"c1\"},\n" +
		"    {\"student\": \"s1\", \"id\": \"r2\", \"course\": \"c2\"}\n  ],\n" +
		"  \"enrollments\": [],\n  \"waivers\": [],\n  \"nextResultSeq\": 0\n}\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errText, code := runCLI(t, file, "show", "s1")
	if code != 0 || strings.Contains(errText, "内容损坏") {
		t.Fatalf("不同对象的同名字段应正常读取，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(out, "数学") || !strings.Contains(out, "物理") {
		t.Fatalf("两门课程都应正常显示，out=%q", out)
	}
}

// TestCLIDuplicateFieldFixedThenNormal 修正重复字段后，原入口恢复正常：
// 学分计算与免修历史规则不变。
func TestCLIDuplicateFieldFixedThenNormal(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	bad := "{\"version\":1,\"courses\":[{\"id\":\"c1\",\"name\":\"数学\"," +
		"\"credit\":4,\"credit\":9,\"open\":true}],\"students\":[],\"requirements\":[]," +
		"\"enrollments\":[],\"waivers\":[],\"nextResultSeq\":0}\n"
	if err := os.WriteFile(file, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errText, code := runCLI(t, file, "list-courses"); code != exitFile {
		t.Fatalf("修正前应拒绝，code=%d err=%q", code, errText)
	}
	fixed := "{\"version\":1,\"courses\":[{\"id\":\"c1\",\"name\":\"数学\"," +
		"\"credit\":4,\"open\":true}],\"students\":[],\"requirements\":[]," +
		"\"enrollments\":[],\"waivers\":[],\"nextResultSeq\":0}\n"
	if err := os.WriteFile(file, []byte(fixed), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errText, code := runCLI(t, file, "list-courses")
	if code != 0 {
		t.Fatalf("修正后应正常列出课程，code=%d err=%q", code, errText)
	}
	if !strings.Contains(out, "4 学分") || strings.Contains(out, "9 学分") {
		t.Fatalf("修正后应按 4 学分读取，out=%q", out)
	}
}
