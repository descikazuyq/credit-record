package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归命令行层面对“无效 Unicode 文字”记录的处理：任何需要打开记录
// 文件的命令都必须在 Load 阶段统一失败——退出码 2、标准错误明确指出目标
// 记录文件包含无效的 Unicode 文字、标准输出不得出现核对结果或任何登记/
// 保存成功的反馈，原文件逐字节保留。合法文字（中文、直接写入的补充平面
// 字符、完整代理项转义对、用户自己写入的 U+FFFD、转义控制字符）保持原有
// 命令用法与输出兼容。

// 结构完整的记录：s1 有一门 4 学分课程并已通过，另有一条已拒绝免修历史，
// 但该历史的依据里写了未配对的高位代理项转义。
const cliCorruptRejectedBasis = "{\n  \"version\": 1,\n" +
	"  \"courses\": [{\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"open\": true}],\n" +
	"  \"students\": [{\"id\": \"s1\"}],\n" +
	"  \"requirements\": [{\"id\": \"r1\", \"student\": \"s1\", \"course\": \"c1\"}],\n" +
	"  \"enrollments\": [{\"id\": \"e1\", \"student\": \"s1\", \"req\": \"r1\"," +
	" \"term\": \"2024春\", \"result\": \"passed\", \"resultSeq\": 1}],\n" +
	"  \"waivers\": [{\"id\": \"wX\", \"student\": \"s1\", \"req\": \"nope\"," +
	" \"basis\": \"获奖\\uD800材料\", \"status\": \"rejected\"," +
	" \"reason\": \"目标要求 nope 不存在或不属于该学生\"}],\n" +
	"  \"nextResultSeq\": 1\n}\n"

// 损坏出现在本次核对根本不涉及的课程名称里（孤立低位代理项）。
const cliCorruptUnrelatedCourse = "{\n  \"version\": 1,\n" +
	"  \"courses\": [{\"id\": \"c1\", \"name\": \"数学\", \"credit\": 4, \"open\": true}," +
	"{\"id\": \"c9\", \"name\": \"课\\uDC00程\", \"credit\": 2, \"open\": true}],\n" +
	"  \"students\": [{\"id\": \"s1\"}],\n" +
	"  \"requirements\": [{\"id\": \"r1\", \"student\": \"s1\", \"course\": \"c1\"}],\n" +
	"  \"enrollments\": [], \"waivers\": [], \"nextResultSeq\": 0\n}\n"

// 合法记录：直接写入补充平面字符、完整高低代理项转义对、用户自己写入的
// U+FFFD 与转义控制字符同时出现，读取与落盘都应保留原意。
const cliLegalUnicode = "{\n  \"version\": 1,\n" +
	"  \"courses\": [{\"id\": \"c1\", \"name\": \"数学😀\", \"credit\": 4, \"open\": true}],\n" +
	"  \"students\": [{\"id\": \"s1\"}],\n" +
	"  \"requirements\": [{\"id\": \"r1\", \"student\": \"s1\", \"course\": \"c1\"}],\n" +
	"  \"enrollments\": [{\"id\": \"e1\", \"student\": \"s1\", \"req\": \"r1\"," +
	" \"term\": \"2024春\\t上\", \"result\": \"passed\", \"resultSeq\": 1}],\n" +
	"  \"waivers\": [{\"id\": \"wX\", \"student\": \"s1\", \"req\": \"nope\"," +
	" \"basis\": \"\\uD83D\\uDE00竞赛材料\\uFFFD\", \"status\": \"rejected\"," +
	" \"reason\": \"目标要求 nope 不存在或不属于该学生\"}],\n" +
	"  \"nextResultSeq\": 1\n}\n"

// TestCLIInvalidUnicodeAllCommandsFailClosed 无论只读（check/show、
// list-courses）还是写入类（登记、提交、免修、撤销）命令，打开含无效
// Unicode 的记录都必须得到统一结果：退出码 2、stdout 为空、stderr 点名
// 文件并说明存在无效的 Unicode 文字，且原文件保持不变。
func TestCLIInvalidUnicodeAllCommandsFailClosed(t *testing.T) {
	corruptByte := "{\n  \"version\": 1,\n" +
		"  \"students\": [{\"id\": \"s1" + "\xff" + "\"}],\n" +
		"  \"courses\": [], \"requirements\": [], \"enrollments\": []," +
		" \"waivers\": [], \"nextResultSeq\": 0\n}\n"
	// 字段名里出现未配对代理项：字段名称与字段值同等对待。
	corruptKey := "{\n  \"version\": 1,\n" +
		"  \"students\": [{\"id\": \"s1\"}],\n" +
		"  \"waivers\": [{\"id\": \"w1\", \"student\": \"s1\", \"req\": \"nope\"," +
		" \"basis\": \"材料\", \"\\uD800\": \"x\", \"status\": \"rejected\"," +
		" \"reason\": \"目标要求 nope 不存在或不属于该学生\"}],\n" +
		"  \"nextResultSeq\": 0\n}\n"
	cases := map[string]string{
		"已拒绝免修依据含孤立代理项": cliCorruptRejectedBasis,
		"无关课程名称含孤立代理项":  cliCorruptUnrelatedCourse,
		"学生编号含非法UTF8字节": corruptByte,
		"字段名含孤立代理项":     corruptKey,
	}
	commands := [][]string{
		{"check", "s1"},
		{"show", "s1"},
		{"list-courses"},
		{"student", "s9"},
		{"course", "c9", "物理", "3"},
		{"course-close", "c1"},
		{"course-open", "c1"},
		{"req", "s1", "r9", "c1"},
		{"enroll", "s1", "r1", "2025春", "e9"},
		{"pass", "s1", "e1"},
		{"fail", "s1", "e1"},
		{"waiver", "s1", "r1", "w9", "新依据"},
		{"revoke-waiver", "s1", "w9", "撤销原因"},
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			for _, args := range commands {
				file := filepath.Join(t.TempDir(), "records.json")
				if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				out, errText, code := runCLI(t, file, args...)
				if code != exitFile {
					t.Fatalf("命令 %v 遇无效 Unicode 应退出码 %d，code=%d out=%q err=%q",
						args, exitFile, code, out, errText)
				}
				if out != "" {
					t.Fatalf("命令 %v 不得输出核对结果或成功反馈，out=%q", args, out)
				}
				if !strings.Contains(errText, file) ||
					!strings.Contains(errText, "无效的 Unicode 文字") {
					t.Fatalf("命令 %v 的标准错误点名文件并指出无效 Unicode 文字，err=%q",
						args, errText)
				}
				got, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != content {
					t.Fatalf("命令 %v 不得改动原文件\nwant=%q\n got=%q",
						args, content, got)
				}
			}
		})
	}
}

// TestCLIInvalidUnicodeRejectsWholeFile 不能只跳过有问题的那一条记录：
// 即使查询的是其他学生、其他编号，损坏文件也必须整体拒绝；也不能把它
// 当作空记录（例如输出“尚无课程”）。
func TestCLIInvalidUnicodeRejectsWholeFile(t *testing.T) {
	for _, content := range []string{cliCorruptRejectedBasis, cliCorruptUnrelatedCourse} {
		file := filepath.Join(t.TempDir(), "records.json")
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"check", "s1"},
			{"check", "s2"}, // 文件里根本没有的学生
			{"show", "s1"},
			{"show", "ghost"},
			{"list-courses"},
		} {
			out, errText, code := runCLI(t, file, args...)
			if code != exitFile {
				t.Fatalf("命令 %v 必须随整份损坏文件拒绝，code=%d out=%q err=%q",
					args, code, out, errText)
			}
			if out != "" {
				t.Fatalf("命令 %v 不得给出任何业务输出，out=%q", args, out)
			}
			if !strings.Contains(errText, "无效的 Unicode 文字") {
				t.Fatalf("命令 %v 应说明无效 Unicode 文字，err=%q", args, errText)
			}
		}
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != content {
			t.Fatal("拒绝期间原文件必须逐字节保留")
		}
	}
}

// TestCLILegalUnicodeKeepsWorking 合法文字保持现有命令用法与输出：核对
// 学分、已拒绝免修历史、课程名称与依据中的补充平面字符和用户写入的
// U+FFFD 都按原意显示；随后触发保存（新登记一条），重新打开后这些文字
// 仍在，且新免修可以正常保存反馈。
func TestCLILegalUnicodeKeepsWorking(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(file, []byte(cliLegalUnicode), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") || !strings.Contains(out, "wX") {
		t.Fatalf("合法 Unicode 记录核对应正常，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 ||
		!strings.Contains(out, "数学😀") ||
		!strings.Contains(out, "😀竞赛材料�") ||
		!strings.Contains(out, "2024春\t上") {
		t.Fatalf("合法文字（补充平面字符、U+FFFD、转义控制字符）应原样显示，code=%d out=%q",
			code, out)
	}

	// 再登记一门带补充平面字符名称的课程，触发正常保存。
	if out, _, code := runCLI(t, file, "course", "c2", "线性代数𠀀", "3"); code != 0 {
		t.Fatalf("合法 Unicode 新登记应成功，code=%d out=%q", code, out)
	}

	// 重新打开进程：保存后的文件中各种合法文字仍可读取。
	out, _, code = runCLI(t, file, "list-courses")
	if code != 0 || !strings.Contains(out, "线性代数𠀀") || !strings.Contains(out, "数学😀") {
		t.Fatalf("保存后合法 Unicode 课程名称应仍可读取，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 || !strings.Contains(out, "😀竞赛材料�") {
		t.Fatalf("保存后已拒绝免修依据的合法文字应保留，code=%d out=%q", code, out)
	}

	// 新免修（依据含补充平面字符）应得到正常的“已保存”类反馈并可再读取。
	if out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w2", "获奖🏆证明"); code != 0 ||
		!strings.Contains(out, "免修 w2 有效") {
		t.Fatalf("合法 Unicode 免修应正常保存，code=%d out=%q", code, out)
	}
	if out, _, code = runCLI(t, file, "show", "s1"); code != 0 ||
		!strings.Contains(out, "获奖🏆证明") {
		t.Fatalf("新保存免修的合法依据应可再读取，code=%d out=%q", code, out)
	}
}
