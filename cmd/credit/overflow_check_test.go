package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLICheckTotalCreditsOverflow 通过命令行建立两门各自合法的课程：
// 学分为 9223372036854775807 与 1，两项要求都满足时总学分超出 64 位
// 整数范围。核对必须以退出码 1 拒绝，只在标准错误点名学生与溢出事实，
// 标准输出不出现正常核对报告；只读、不改文件，show 仍能查看原始记录。
func TestCLICheckTotalCreditsOverflow(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c-big", "上限课", "9223372036854775807"},
		{"course", "c-one", "一学分课", "1"},
		{"req", "s1", "r1", "c-big"},
		{"req", "s1", "r2", "c-one"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 只有第一项满足：总学分恰为上限，正常核对并显示完整数值。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：9223372036854775807") {
		t.Fatalf("总学分恰为上限时应正常核对，code=%d out=%q", code, out)
	}

	// 第二项只有选课记录时也不应触发超限。
	if _, _, code := runCLI(t, file, "enroll", "s1", "r2", "2024春", "e2"); code != 0 {
		t.Fatal("选课登记失败")
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：9223372036854775807") ||
		!strings.Contains(out, "未满足要求：[r2]") {
		t.Fatalf("第二项仅选课时不应超限且应列为未满足，code=%d out=%q", code, out)
	}

	// 第二项通过：MaxInt+1，核对被拒绝。该写入单独完成，随后的超限核对
	// 必须只读、不再改动文件。
	if _, _, code := runCLI(t, file, "pass", "s1", "e2"); code != 0 {
		t.Fatal("提交通过失败")
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	out, errText, code := runCLI(t, file, "check", "s1")
	if code != 1 {
		t.Fatalf("超限时应按业务规则拒绝且退出码 1，code=%d out=%q err=%q", code, out, errText)
	}
	if out != "" || strings.Contains(out, "总学分") {
		t.Fatalf("超限时标准输出不应出现正常核对报告或任何总学分，out=%q", out)
	}
	if !strings.Contains(errText, "s1") {
		t.Fatalf("标准错误应点名学生编号，err=%q", errText)
	}
	if !strings.Contains(errText, "9223372036854775807") ||
		!strings.Contains(errText, "超出") {
		t.Fatalf("标准错误应指出总学分超出可表示范围及上限，err=%q", errText)
	}
	if strings.Contains(errText, "损坏") {
		t.Fatalf("各门课程学分合法，不能说成记录文件损坏，err=%q", errText)
	}
	// 不能给出回绕后的负数或任何截断/部分累加数字。
	if strings.Contains(errText, "-9223372036854775808") {
		t.Fatalf("不能暴露回绕结果，err=%q", errText)
	}

	// 只读拒绝：记录文件内容不变，show 仍能查看该学生的原始记录。
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("超限核对不得改动记录文件\nbefore=%s\nafter=%s", before, after)
	}
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 || !strings.Contains(show, "结果：通过") ||
		!strings.Contains(show, "9223372036854775807") {
		t.Fatalf("show 应仍能查看原始记录与学分，code=%d show=%q", code, show)
	}
}

// TestCLICheckOverflowPerStudent 记录文件中一名学生超限时，另一名学生的
// 查询不受影响；查询不存在学生仍走原有的不存在行为。
func TestCLICheckOverflowPerStudent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	for _, st := range [][]string{
		{"student", "s1"},
		{"student", "s2"},
		{"course", "c-big", "上限课", "9223372036854775807"},
		{"course", "c-one", "一学分课", "1"},
		{"req", "s1", "r1", "c-big"},
		{"req", "s1", "r2", "c-one"},
		{"req", "s2", "q1", "c-one"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
		{"enroll", "s1", "r2", "2024春", "e2"},
		{"pass", "s1", "e2"},
		{"enroll", "s2", "q1", "2024春", "f1"},
		{"pass", "s2", "f1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	out, errText, code := runCLI(t, file, "check", "s1")
	if code != 1 || out != "" || !strings.Contains(errText, "s1") {
		t.Fatalf("超限学生 s1 应退出码 1 且无标准输出，code=%d out=%q err=%q",
			code, out, errText)
	}
	// 另一名学生正常核对。
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：1") {
		t.Fatalf("s2 不应因 s1 超限被拒绝，code=%d out=%q", code, out)
	}
	// 不存在学生仍为原有的业务错误（退出码 1、点名不存在），不能与超限混淆。
	out, errText, code = runCLI(t, file, "check", "ghost")
	if code != 1 || out != "" || !strings.Contains(errText, "不存在") {
		t.Fatalf("不存在学生行为应保持不变，code=%d out=%q err=%q", code, out, errText)
	}
}
