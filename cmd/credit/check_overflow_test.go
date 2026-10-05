package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 两名学生、两项要求的场景：c1 学分为 int 上限，c2 学分为 1。
func setupOverflowFile(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "records.json")
	max := strconv.Itoa(math.MaxInt)
	steps := [][]string{
		{"student", "s1"},
		{"student", "s2"},
		{"course", "c1", "数学", max},
		{"course", "c2", "物理", "1"},
		{"req", "s1", "r1", "c1"},
		{"req", "s1", "r2", "c2"},
		{"req", "s2", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
		{"enroll", "s2", "r1", "2024春", "e9"},
		{"pass", "s2", "e9"},
	}
	for _, st := range steps {
		if out, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 失败 code=%d out=%q err=%q", st, code, out, errText)
		}
	}
	return file
}

// 总学分超出 int 上限时：退出码 1，标准错误点名学生与可表示范围，
// 标准输出不展示任何正常核对报告，记录文件保持不变。
func TestCLICheckOverflowRejected(t *testing.T) {
	file := setupOverflowFile(t)

	// 第二项仅选课：不计学分，不触发超限。
	if _, _, code := runCLI(t, file, "enroll", "s1", "r2", "2024春", "e2"); code != 0 {
		t.Fatal("选课 e2 失败")
	}
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分："+strconv.Itoa(math.MaxInt)) {
		t.Fatalf("第二项仅选课时应正常核对并显示完整上限数值，code=%d out=%q", code, out)
	}
	// 第二项未通过：同样不触发超限。
	if _, _, code := runCLI(t, file, "fail", "s1", "e2"); code != 0 {
		t.Fatal("提交未通过失败")
	}
	if out, _, code := runCLI(t, file, "check", "s1"); code != 0 ||
		!strings.Contains(out, "总学分："+strconv.Itoa(math.MaxInt)) {
		t.Fatalf("第二项未通过时应正常核对，code=%d out=%q", code, out)
	}

	// 第二项通过：总和超出上限，核对必须被拒绝。
	if _, _, code := runCLI(t, file, "enroll", "s1", "r2", "2025春", "e3"); code != 0 {
		t.Fatal("选课 e3 失败")
	}
	if _, _, code := runCLI(t, file, "pass", "s1", "e3"); code != 0 {
		t.Fatal("提交通过失败")
	}

	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	out, errText, code := runCLI(t, file, "check", "s1")
	if code != 1 {
		t.Fatalf("超限核对应以退出码 1 拒绝，得到 code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(errText, "s1") || !strings.Contains(errText, "超出") ||
		!strings.Contains(errText, strconv.Itoa(math.MaxInt)) {
		t.Fatalf("标准错误应指出学生编号及总学分超出可表示范围，err=%q", errText)
	}
	if strings.Contains(out, "总学分") || strings.Contains(out, "核对结果") ||
		strings.Contains(out, "课程要求") {
		t.Fatalf("超限时不应在标准输出展示正常核对报告，out=%q", out)
	}
	if strings.Contains(errText, "损坏") {
		t.Fatalf("课程学分本身合法，不能说成记录文件损坏，err=%q", errText)
	}

	// 核对是只读的：被拒绝后文件内容不变。
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("超限核对不得修改记录文件")
	}

	// 拒绝不影响后续查询：show 仍展示原始记录，其他学生核对不受影响。
	if out, _, code := runCLI(t, file, "show", "s1"); code != 0 ||
		!strings.Contains(out, "要求 r2") {
		t.Fatalf("超限后 show 仍应可用，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分："+strconv.Itoa(math.MaxInt)) {
		t.Fatalf("另一名学生不应因 s1 超限被拒绝，code=%d out=%q", code, out)
	}
}

// 总学分恰好等于 int 上限时正常核对，标准输出展示完整数值。
func TestCLICheckTotalAtMaxIntOK(t *testing.T) {
	file := setupOverflowFile(t)
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("总学分等于上限应正常核对，code=%d", code)
	}
	want := fmt.Sprintf("总学分：%d", math.MaxInt)
	if !strings.Contains(out, want) {
		t.Fatalf("应显示完整上限数值 %q，out=%q", want, out)
	}
}
