package main

import (
	"os"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“pass/fail 按完整学生编号与修读编号确定归属”。
// 含前后空白的编号只能经合法记录文件产生（登记入口会修剪），所以用例直接
// 写出结构完整、引用齐全的记录文件，每条命令都重新打开进程访问它：
//   - 给带前后空白编号的学生提交结果：只更新其本人修读，check 按本人要求
//     获得学分并以本人修读说明来源；不带空白学生的状态、学分、来源不变；
//   - 同一学生名下 "e1" 与 " e1 " 是两次独立修读，分别提交、互不冲突；
//   - 完整学生编号不存在、或学生存在但名下没有完整修读编号时，即使去掉
//     空白恰好能对上另一条记录，也必须退出码 1、不输出成功提示、不借用、
//     不补建，原记录文件逐字节保留；
//   - 普通空格、制表符、全角空格都不能被忽略；普通不含空白的编号照常使用。

// whitespaceDiskRecord 构造两名学生 "s1" 与 " s1 " 的合法记录：
// s1 名下 r1->c1（4 学分）带修读 e1，r2->c2（3 学分）带修读 " e1 "；
// " s1 " 名下 r1->c1 带修读 e1。三份修读初始均为选课。
func whitespaceDiskRecord() *diskRecord {
	return &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
			{ID: "r2", Student: "s1", Course: "c2"},
			{ID: "r1", Student: " s1 ", Course: "c1"},
		},
		Enrollments: []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
			{ID: " e1 ", Student: "s1", Req: "r2", Term: "2024秋", Result: "enrolled"},
			{ID: "e1", Student: " s1 ", Req: "r1", Term: "2024春", Result: "enrolled"},
		},
	}
}

// TestCLIResultPaddedStudentTargetsOnlyThatStudent 题目主场景：给 " s1 "
// 提交通过，只有这名学生的 e1 通过；核对时按其本人要求获得学分、来源是
// 本人的 e1。"s1" 的修读状态、学分与来源保持原样。
func TestCLIResultPaddedStudentTargetsOnlyThatStudent(t *testing.T) {
	file, _ := writeDiskRecord(t, whitespaceDiskRecord())

	out, errText, code := runCLI(t, file, "pass", " s1 ", "e1")
	if code != exitOK {
		t.Fatalf("给 \" s1 \" 提交通过应成功，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(out, "修读 e1 结果已提交：通过") {
		t.Fatalf("应输出提交成功提示，out=%q", out)
	}

	// " s1 "：4 学分、要求满足、来源本人 e1。
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != exitOK {
		t.Fatalf("核对 \" s1 \" 应成功，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("\" s1 \" 应凭本人 e1 得 4 学分并以其说明来源，out=%q", out)
	}

	// "s1"：0 学分、r1 未满足，show 中本人 e1 仍是选课。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != exitOK {
		t.Fatalf("核对 s1 应成功，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1 r2]") {
		t.Fatalf("s1 应保持 0 学分、两项要求未满足，out=%q", out)
	}
	if strings.Contains(out, "通过修读") {
		t.Fatalf("s1 不应有任何通过修读来源，out=%q", out)
	}
	out, _, code = runCLI(t, file, "show", "s1")
	if code != exitOK {
		t.Fatalf("show s1 应成功，code=%d", code)
	}
	if !strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("s1 的 e1 必须保持选课，out=%q", out)
	}

	// 重复提交相同结果：幂等命中本人 e1，不再写文件。
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	out, _, code = runCLI(t, file, "pass", " s1 ", "e1")
	if code != exitOK || !strings.Contains(out, "已提交过相同结果") {
		t.Fatalf("\" s1 \" 重复通过应幂等返回原记录，code=%d out=%q", code, out)
	}
	assertFileByteIdentical(t, file, before, "幂等重复提交")

	// s1 本人的 e1 仍可独立通过，不与 " s1 " 的 e1 构成重复或冲突。
	if _, _, code := runCLI(t, file, "pass", "s1", "e1"); code != exitOK {
		t.Fatalf("s1 提交本人 e1 通过应独立成功，code=%d", code)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != exitOK || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("s1 也通过后应凭本人 e1 得 4 学分，code=%d out=%q", code, out)
	}
}

// TestCLIResultPaddedEnrollmentIDSplit 同一学生名下 e1 与 " e1 " 分别
// 处理：提交一份不改变另一份，两次独立修读不被误认为重复提交或结果冲突。
func TestCLIResultPaddedEnrollmentIDSplit(t *testing.T) {
	file, _ := writeDiskRecord(t, whitespaceDiskRecord())

	// 先对 " e1 "（指向 r2/c2，3 学分）提交未通过。
	if out, errText, code := runCLI(t, file, "fail", "s1", " e1 "); code != exitOK {
		t.Fatalf("对 \" e1 \" 提交未通过应成功，code=%d out=%q err=%q", code, out, errText)
	}
	out, _, code := runCLI(t, file, "check", "s1")
	if code != exitOK || !strings.Contains(out, "总学分：0") {
		t.Fatalf("未通过不计学分，code=%d out=%q", code, out)
	}

	// 再对 e1 提交通过：必须是独立修读，不能误判重复提交或结果冲突。
	if out, errText, code := runCLI(t, file, "pass", "s1", "e1"); code != exitOK {
		t.Fatalf("对独立的 e1 提交通过应成功，code=%d out=%q err=%q", code, out, errText)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != exitOK {
		t.Fatalf("核对 s1 应成功，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("s1 应只凭 e1 得 4 学分，out=%q", out)
	}
	// r2（" e1 " 未通过）仍未满足，且未通过修读不出现在通过历史里。
	if !strings.Contains(out, "未满足要求：[r2]") {
		t.Fatalf("r2 应因 \" e1 \" 未通过而未满足，out=%q", out)
	}

	// 两份各自改结果都按本人记录拒绝，不能借另一份的结果。
	if _, errText, code := runCLI(t, file, "pass", "s1", " e1 "); code != exitRejected ||
		!strings.Contains(errText, " e1 ") {
		t.Fatalf("\" e1 \" 未通过后改通过应退出码 1 并点名该修读，code=%d err=%q",
			code, errText)
	}
	if _, errText, code := runCLI(t, file, "fail", "s1", "e1"); code != exitRejected ||
		!strings.Contains(errText, "e1") {
		t.Fatalf("e1 通过后改未通过应退出码 1 并点名 e1，code=%d err=%q", code, errText)
	}
}

// TestCLIResultWhitespaceNotFoundRejectsAndKeepsFile 完整编号找不到时明确
// 报告学生/修读不存在：退出码 1、无成功提示、不借用去空白后的记录、不补建，
// 记录文件逐字节保持原样。
func TestCLIResultWhitespaceNotFoundRejectsAndKeepsFile(t *testing.T) {
	// 完整学生编号不存在：普通空格、制表符、全角空格的变体都不是 s1 或 " s1 "。
	for _, student := range []string{"  s1  ", "\ts1", "s1\t", "　s1　", " \ts1 \t"} {
		file, raw := writeDiskRecord(t, whitespaceDiskRecord())
		out, errText, code := runCLI(t, file, "pass", student, "e1")
		if code != exitRejected {
			t.Fatalf("学生 %q 不存在应退出码 1，code=%d out=%q err=%q",
				student, code, out, errText)
		}
		if out != "" {
			t.Fatalf("学生 %q 被拒绝时不得输出提交成功提示，out=%q", student, out)
		}
		if !strings.Contains(errText, "学生") ||
			!strings.Contains(errText, student) || !strings.Contains(errText, "不存在") {
			t.Fatalf("应明确报告完整学生编号 %q 不存在，err=%q", student, errText)
		}
		assertFileByteIdentical(t, file, raw, "学生不存在被拒绝")
	}

	// 学生存在但名下没有完整修读编号：" s1 " 名下只有 e1，没有 " e1 "
	// （那是 s1 的）；制表符/全角空格变体同样不是 e1。
	for _, enr := range []string{" e1 ", "\te1", "e1\t", "　e1　"} {
		file, raw := writeDiskRecord(t, whitespaceDiskRecord())
		out, errText, code := runCLI(t, file, "pass", " s1 ", enr)
		if code != exitRejected {
			t.Fatalf("\" s1 \" 名下修读 %q 不存在应退出码 1，code=%d out=%q err=%q",
				enr, code, out, errText)
		}
		if out != "" {
			t.Fatalf("修读 %q 被拒绝时不得输出提交成功提示，out=%q", enr, out)
		}
		if !strings.Contains(errText, " s1 ") ||
			!strings.Contains(errText, enr) || !strings.Contains(errText, "不存在") {
			t.Fatalf("应明确报告 \" s1 \" 名下不存在完整修读编号 %q，err=%q",
				enr, errText)
		}
		assertFileByteIdentical(t, file, raw, "名下无此修读被拒绝")
	}
}

// TestCLIResultWhitespaceRecordsNotCorrupt 含前后空白编号的记录本身合法：
// 查询命令正常区分两名学生，拒绝读取类的退出码 2 绝不能出现。
func TestCLIResultWhitespaceRecordsNotCorrupt(t *testing.T) {
	file, _ := writeDiskRecord(t, whitespaceDiskRecord())

	for _, student := range []string{"s1", " s1 "} {
		if _, errText, code := runCLI(t, file, "show", student); code != exitOK {
			t.Fatalf("含空白编号的合法文件必须可正常查询 %q，code=%d err=%q",
				student, code, errText)
		}
		if _, _, code := runCLI(t, file, "check", student); code != exitOK {
			t.Fatalf("含空白编号的合法文件必须可正常核对 %q，code=%d", student, code)
		}
	}

	// 两份 e1 初始都是选课，查询能把两名学生区分开。
	out, _, code := runCLI(t, file, "show", " s1 ")
	if code != exitOK || !strings.Contains(out, "学生  s1 ") ||
		!strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("show 应保留 \" s1 \" 编号原文并列出本人 e1，code=%d out=%q", code, out)
	}
}
