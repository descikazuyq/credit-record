package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“记录文件中的字符串必须能还原为合法 Unicode 文字”。
// 标准库解码会把非法 UTF-8 字节与未配对的高/低位代理项转义静默替换成
// U+FFFD（“�”），替换后的编号或依据绝不能被当成正常内容办理业务。这类
// 文件无法经正常命令产生，因此用例直接写出记录文件，每条断言都重新打开
// 进程访问它：
//   - 无论问题在学生编号、课程名称、已拒绝免修的依据还是字段名里，所有
//     命令（只读核对、历史查看、列课程、登记、申请免修）都在读取阶段以
//     退出码 2 拒绝：标准输出没有任何业务内容，标准错误点名记录文件、
//     说明内容损坏与无效的 Unicode 文字，原文件逐字节保留；
//   - 哪怕问题只在本次核对不涉及的课程名称里，也不能跳过该条记录再给出
//     核对结果；
//   - 合法文字（完整代理项对、直接写入的补充平面字符、用户确实写入的
//     “�”）仍按原规则正常使用，命令用法与输出保持兼容。

// uesc 拼出一个 JSON Unicode 转义序列：uesc("D800") 返回 6 个字符
// （反斜线、u、D、8、0、0）。用函数拼接是为了避免测试源码直接书写转义
// 文本时被编辑器或工具链转换。
func uesc(hex string) string { return string(rune(0x5C)) + "u" + hex }

// unicodeCLIRecord 返回一份结构完整、引用齐全的记录：学生 s1 的要求 r1
// 指向 4 学分课程 c1 并已有通过修读 e1；另有一门与本次核对无关的课程 c2。
func unicodeCLIRecord() string {
	return `{
  "version": 1,
  "courses": [
    {"id": "c1", "name": "高等数学", "credit": 4, "open": true},
    {"id": "c2", "name": "线性代数", "credit": 3, "open": true}
  ],
  "students": [
    {"id": "s1"}
  ],
  "requirements": [
    {"id": "r1", "student": "s1", "course": "c1"}
  ],
  "enrollments": [
    {"id": "e1", "student": "s1", "req": "r1", "term": "2024春", "result": "passed", "resultSeq": 1}
  ],
  "waivers": [],
  "nextResultSeq": 1
}
`
}

// writeRawDiskRecord 把原始 JSON 文本（可能含非法 UTF-8 字节）写入临时
// 记录文件，返回路径与原始字节。
func writeRawDiskRecord(t *testing.T, content string) (string, []byte) {
	t.Helper()
	raw := []byte(content)
	file := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatalf("写入记录文件失败：%v", err)
	}
	return file, raw
}

// replaceOnceCLI 把 base 中唯一出现的 old 替换为 rep。
func replaceOnceCLI(t *testing.T, base, old, rep string) string {
	t.Helper()
	if strings.Count(base, old) != 1 {
		t.Fatalf("基础记录中 %q 应恰好出现一次", old)
	}
	return strings.Replace(base, old, rep, 1)
}

// TestCLIInvalidUnicodeRejectsAllCommands 记录文件含无效 Unicode 文字时，
// 所有需要打开文件的命令都在读取阶段以退出码 2 拒绝：标准输出不出现核对
// 结果、登记成功或免修已保存的反馈，标准错误明确指出记录文件包含无效的
// Unicode 文字，原文件内容保持不变。
func TestCLIInvalidUnicodeRejectsAllCommands(t *testing.T) {
	hi, lo := uesc("D800"), uesc("DC00")
	bad := string([]byte{0xFF})
	cases := map[string]func(t *testing.T, base string) string{
		"已拒绝免修的依据含孤立高位代理项": func(t *testing.T, base string) string {
			return replaceOnceCLI(t, base, `"waivers": []`,
				`"waivers": [{"id": "w1", "student": "s1", "req": "r1", `+
					`"basis": "材料`+hi+`", "status": "rejected", `+
					`"reason": "免修依据为空"}]`)
		},
		"核对不涉及的课程名称含孤立低位代理项": func(t *testing.T, base string) string {
			return replaceOnceCLI(t, base, `"name": "线性代数"`, `"name": "线性`+lo+`代数"`)
		},
		"学生编号含非法 UTF-8 字节": func(t *testing.T, base string) string {
			return replaceOnceCLI(t, base, `"id": "s1"`, `"id": "s`+bad+`1"`)
		},
		"字段名含两个相连高位代理项": func(t *testing.T, base string) string {
			return replaceOnceCLI(t, base, `"students"`, `"stud`+hi+hi+`ents"`)
		},
	}
	commands := [][]string{
		{"check", "s1"},
		{"show", "s1"},
		{"list-courses"},
		{"student", "s9"},
		{"course", "c9", "新课", "2"},
		{"waiver", "s1", "r1", "w9", "依据"},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			file, raw := writeRawDiskRecord(t, corrupt(t, unicodeCLIRecord()))
			for _, args := range commands {
				out, errText, code := runCLI(t, file, args...)
				if code != exitFile {
					t.Fatalf("%s %v：应退出码 %d，code=%d out=%q err=%q",
						name, args, exitFile, code, out, errText)
				}
				if out != "" {
					t.Fatalf("%s %v：标准输出不应有任何业务内容，out=%q", name, args, out)
				}
				for _, banned := range []string{"总学分", "已登记", "已满足", "有效"} {
					if strings.Contains(out, banned) {
						t.Fatalf("%s %v：标准输出不能出现 %q 类反馈，out=%q",
							name, args, banned, out)
					}
				}
				for _, want := range []string{file, "内容损坏", "无效的 Unicode 文字"} {
					if !strings.Contains(errText, want) {
						t.Fatalf("%s %v：错误输出应包含 %q，err=%q", name, args, want, errText)
					}
				}
			}
			assertFileByteIdentical(t, file, raw, name+"：全部访问后")
		})
	}
}

// TestCLIInvalidUnicodeNotSkippedPerRecord 问题出在一条已拒绝免修的依据里、
// 且本次核对的学生与课程都合法时，也不能只跳过该条记录再给出核对结果：
// 整份文件不可读，check 以退出码 2 结束且不输出任何核对结论。
func TestCLIInvalidUnicodeNotSkippedPerRecord(t *testing.T) {
	base := replaceOnceCLI(t, unicodeCLIRecord(), `"waivers": []`,
		`"waivers": [{"id": "w1", "student": "s1", "req": "r1", `+
			`"basis": "材料`+uesc("D800")+`", "status": "rejected", `+
			`"reason": "免修依据为空"}]`)
	file, raw := writeRawDiskRecord(t, base)

	out, errText, code := runCLI(t, file, "check", "s1")
	if code != exitFile {
		t.Fatalf("不能只跳过损坏记录再核对，应退出码 %d，code=%d out=%q err=%q",
			exitFile, code, out, errText)
	}
	if out != "" || strings.Contains(out, "总学分") {
		t.Fatalf("不得给出任何核对结果，out=%q", out)
	}
	if !strings.Contains(errText, "无效的 Unicode 文字") {
		t.Fatalf("错误应指出无效的 Unicode 文字，err=%q", errText)
	}
	assertFileByteIdentical(t, file, raw, "check 之后：")
}

// TestCLIValidUnicodeTextWorksNormally 合法文字保持兼容：完整代理项转义对
// 还原为补充平面字符参与核对输出，用户确实写入的“�”是合法文字、可按原
// 编号查询，只读访问不改动文件。
func TestCLIValidUnicodeTextWorksNormally(t *testing.T) {
	base := unicodeCLIRecord()
	base = replaceOnceCLI(t, base, `"name": "高等数学"`,
		`"name": "`+uesc("D83D")+uesc("DE00")+`数学"`)
	base = replaceOnceCLI(t, base, `{"id": "s1"}`, `{"id": "s1"}, {"id": "s�2"}`)
	file, raw := writeRawDiskRecord(t, base)

	out, errText, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("合法记录应正常核对，code=%d err=%q", code, errText)
	}
	if !strings.Contains(out, "总学分：4") || !strings.Contains(out, "😀数学") {
		t.Fatalf("完整代理项对应按原意参与核对输出，out=%q", out)
	}

	// 用户确实写入的“�”是合法文字：学生可按原编号查询。
	out, _, code = runCLI(t, file, "show", "s�2")
	if code != 0 || !strings.Contains(out, "学生 s�2") {
		t.Fatalf("含“�”的编号应正常使用，code=%d out=%q", code, out)
	}

	// 只读访问不改动文件。
	assertFileByteIdentical(t, file, raw, "check/show 之后：")

	// 登记新学生等正常命令用法不变。
	out, _, code = runCLI(t, file, "student", "s3")
	if code != 0 || !strings.Contains(out, "已登记学生 s3") {
		t.Fatalf("合法文件上的登记命令应正常，code=%d out=%q", code, out)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"s3"`) || !strings.Contains(string(got), "😀数学") {
		t.Fatalf("登记后文件应同时保留新学生与原有合法文字，got=%q", got)
	}
}
