package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“课程开放状态被显式写成 JSON null 时，整份记录
// 文件按内容损坏拒绝”。null 无法经正常命令产生，因此用例直接写出记录
// 文件，每条断言都重新打开进程访问它：
//   - 只要课程对象显式给出 open/Open/OPEN（含解码后等同的 Unicode 转义
//     写法）而值为 null，所有需要打开文件的命令（只读核对、查看其他学生、
//     列课程、各类登记、恢复开放）都在读取阶段以退出码 2 结束：标准输出
//     没有课程列表、学生核对报告或登记成功提示，标准错误点名记录文件与
//     课程编号并说明课程开放状态为空；
//   - 问题课程尚未被要求引用、或本次查看的是另一名学生，也同样拒绝，不能
//     跳过该课程继续办理；
//   - 恢复开放（course-open）或登记其他对象等需要保存的命令不能借机把
//     false 写回文件：原文件全部字节保持不变，不补填状态、不删除课程；
//   - true/false 仍是合法状态：停开课程仍可建立要求、停开前已有修读仍可
//     提交成绩，只有新增修读被业务规则拒绝（退出码 1，而非文件错误）。

// nullOpenCLIRecord 返回一份结构完整、引用齐全的记录：学生 s1 的要求 r1
// 指向开放的 4 学分课程 c1 并有已通过修读 e1；学生 s2 与课程 c2 与本次
// s1 的核对互不相关。corruptOpen 可把 c2（未被引用）或 c1（被引用）的
// 开放状态改成 null。
func nullOpenCLIRecord() string {
	return `{
  "version": 1,
  "courses": [
    {"id": "c1", "name": "高等数学", "credit": 4, "open": true},
    {"id": "c2", "name": "线性代数", "credit": 3, "open": true}
  ],
  "students": [
    {"id": "s1"},
    {"id": "s2"}
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

// TestCLINullOpenRejectsAllCommands 未被引用的 c2 开放状态为 null：本次
// 核对 s1（只涉及 c1）也要以退出码 2 拒绝。所有命令标准输出为空，标准
// 错误点名文件与 c2，全部访问后原文件逐字节不变。
func TestCLINullOpenRejectsAllCommands(t *testing.T) {
	base := strings.Replace(nullOpenCLIRecord(),
		`{"id": "c2", "name": "线性代数", "credit": 3, "open": true}`,
		`{"id": "c2", "name": "线性代数", "credit": 3, "open": null}`, 1)
	file, raw := writeRawDiskRecord(t, base)

	commands := [][]string{
		{"list-courses"},
		{"check", "s1"}, // 只涉及 c1，仍须拒绝
		{"show", "s1"},
		{"check", "s2"}, // 查看另一名学生
		{"show", "s2"},
		{"student", "s9"},
		{"course", "c9", "新课", "2"},
		{"req", "s1", "r9", "c1"},
		{"enroll", "s1", "r1", "2024春", "e9"},
		{"pass", "s1", "e1"},
		{"waiver", "s1", "r1", "w9", "依据"},
		{"course-close", "c1"},
		{"course-open", "c2"}, // 不能借恢复开放把 null 修成 true/false
	}
	for _, args := range commands {
		out, errText, code := runCLI(t, file, args...)
		if code != exitFile {
			t.Fatalf("%v：应退出码 %d，code=%d out=%q err=%q",
				args, exitFile, code, out, errText)
		}
		if out != "" {
			t.Fatalf("%v：标准输出不应出现课程列表、核对报告或登记成功提示，out=%q",
				args, out)
		}
		for _, want := range []string{file, "内容损坏", "c2", "开放状态为空"} {
			if !strings.Contains(errText, want) {
				t.Fatalf("%v：错误输出应包含 %q，err=%q", args, want, errText)
			}
		}
	}
	assertFileByteIdentical(t, file, raw, "全部命令访问后：")
}

// TestCLINullOpenEscapedKeyAndPosition 字段名用 Unicode 转义写成、且字段
// 位置移动到课程对象最前时，命令行同样识别为开放状态字段并拒绝。
func TestCLINullOpenEscapedKeyAndPosition(t *testing.T) {
	cases := map[string]string{
		"Unicode 转义 open": `{"id": "c1", "name": "高等数学", "credit": 4, "` +
			uesc("006f") + uesc("0070") + uesc("0065") + uesc("006e") + `": null}`,
		"转义大写 Open 写在最前": `{"` +
			uesc("004F") + uesc("0070") + uesc("0065") + uesc("006e") +
			`": null, "id": "c1", "name": "高等数学", "credit": 4}`,
	}
	for name, course := range cases {
		t.Run(name, func(t *testing.T) {
			content := `{"version":1,"courses":[` + course +
				`],"students":[],"requirements":[],"enrollments":[],"waivers":[],"nextResultSeq":0}`
			file, raw := writeRawDiskRecord(t, content)
			for _, args := range [][]string{{"list-courses"}, {"course-open", "c1"}} {
				out, errText, code := runCLI(t, file, args...)
				if code != exitFile || out != "" {
					t.Fatalf("%s %v：应退出码 %d 且无输出，code=%d out=%q err=%q",
						name, args, exitFile, code, out, errText)
				}
				if !strings.Contains(errText, "c1") ||
					!strings.Contains(errText, "开放状态为空") {
					t.Fatalf("%s %v：错误应点名 c1 并说明开放状态为空，err=%q",
						name, args, errText)
				}
			}
			assertFileByteIdentical(t, file, raw, name+"：")
		})
	}
}

// TestCLINullOpenReferencedCourseRejected 被要求与修读引用的课程 c1 开放
// 状态为 null 时，即使是对已有通过修读的幂等提交也不能办理：读取阶段即
// 拒绝，原文件保留。
func TestCLINullOpenReferencedCourseRejected(t *testing.T) {
	base := strings.Replace(nullOpenCLIRecord(),
		`{"id": "c1", "name": "高等数学", "credit": 4, "open": true}`,
		`{"id": "c1", "name": "高等数学", "credit": 4, "open": null}`, 1)
	file, raw := writeRawDiskRecord(t, base)

	for _, args := range [][]string{
		{"check", "s1"},
		{"pass", "s1", "e1"}, // 对已有通过修读的幂等提交
		{"enroll", "s1", "r1", "2025春", "e9"},
	} {
		out, errText, code := runCLI(t, file, args...)
		if code != exitFile || out != "" {
			t.Fatalf("%v：被引用课程状态为空应退出码 %d 且无输出，code=%d out=%q err=%q",
				args, exitFile, code, out, errText)
		}
		if !strings.Contains(errText, "c1") || !strings.Contains(errText, "开放状态为空") {
			t.Fatalf("%v：错误应点名 c1 并说明开放状态为空，err=%q", args, errText)
		}
	}
	assertFileByteIdentical(t, file, raw, "访问被引用 null 状态课程后：")
}

// TestCLIFalseOpenIsLegalClosedCourse false 是合法停开状态：文件正常打开，
// 停开课程仍可建立要求、停开前已有修读仍可提交成绩，只有新增修读按业务
// 规则以退出码 1 拒绝（不是文件损坏）。
func TestCLIFalseOpenIsLegalClosedCourse(t *testing.T) {
	content := `{
  "version": 1,
  "courses": [
    {"id": "c1", "name": "高等数学", "credit": 4, "open": false}
  ],
  "students": [
    {"id": "s1"},
    {"id": "s2"}
  ],
  "requirements": [
    {"id": "r1", "student": "s1", "course": "c1"}
  ],
  "enrollments": [
    {"id": "e1", "student": "s1", "req": "r1", "term": "2024春", "result": "enrolled"}
  ],
  "waivers": [],
  "nextResultSeq": 0
}
`
	file, _ := writeRawDiskRecord(t, content)

	// 列表正常显示停开。
	out, errText, code := runCLI(t, file, "list-courses")
	if code != 0 {
		t.Fatalf("合法停开课程应正常列出，code=%d err=%q", code, errText)
	}
	if !strings.Contains(out, "课程 c1《高等数学》4 学分，状态：停开") {
		t.Fatalf("列表应显示停开，out=%q", out)
	}

	// 停开课程仍可建立要求。
	if _, _, code := runCLI(t, file, "req", "s2", "r1", "c1"); code != 0 {
		t.Fatal("停开课程应允许建立要求")
	}

	// 新增修读按业务规则拒绝：退出码 1（不是文件错误 2）。
	_, errText, code = runCLI(t, file, "enroll", "s2", "r1", "2024春", "e9")
	if code != exitRejected {
		t.Fatalf("停开课程新增修读应按业务规则拒绝（退出码 %d），code=%d err=%q",
			exitRejected, code, errText)
	}
	if !strings.Contains(errText, "已停开") {
		t.Fatalf("拒绝原因应说明课程已停开，err=%q", errText)
	}

	// 停开前已有的修读仍可提交成绩并参与核对。
	if out, _, code := runCLI(t, file, "pass", "s1", "e1"); code != 0 ||
		!strings.Contains(out, "结果已提交：通过") {
		t.Fatalf("停开前已有修读应能提交通过，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") {
		t.Fatalf("停开课程的已有通过修读仍计学分，code=%d out=%q", code, out)
	}
}

// TestCLINullOpenNotConfusedWithLiteralNullText 课程名称里的普通文字
// “null”与最外层 null 列表都不是开放状态空值：文件正常读取使用。
func TestCLINullOpenNotConfusedWithLiteralNullText(t *testing.T) {
	content := `{
  "version": 1,
  "courses": null,
  "students": [],
  "requirements": [],
  "enrollments": [],
  "waivers": [],
  "nextResultSeq": 0
}
`
	file, _ := writeRawDiskRecord(t, content)
	out, errText, code := runCLI(t, file, "list-courses")
	if code != 0 {
		t.Fatalf("最外层 null 课程列表按空列表读取，code=%d err=%q", code, errText)
	}
	if !strings.Contains(out, "（尚无课程）") {
		t.Fatalf("应显示尚无课程，out=%q", out)
	}

	// 登记一门名称含“null”的课程并核对列表，随后再次打开不应被判损坏。
	if _, _, code := runCLI(t, file, "course", "c1", "null 概念课", "4"); code != 0 {
		t.Fatal("登记名称含 null 的课程应成功")
	}
	out, _, code = runCLI(t, file, "list-courses")
	if code != 0 || !strings.Contains(out, "null 概念课") ||
		!strings.Contains(out, "状态：开放") {
		t.Fatalf("名称中的 null 文字应原样显示且课程开放，code=%d out=%q", code, out)
	}
}
