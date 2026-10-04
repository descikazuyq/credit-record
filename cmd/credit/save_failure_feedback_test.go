package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“保存阶段失败时的命令反馈”：业务结果在变更真正写入记录文件之前
// 不得出现在标准输出。每条断言都重新打开进程访问记录文件：
//   - 首次建立记录时父目录不存在：登记/更新/提交/撤销类命令必须退出码 2、
//     标准输出为空（不能先显示“已登记学生”），标准错误明确指出保存失败并
//     点名目标记录文件，不能出现“已从空记录开始并创建”，目标文件不得被创建；
//   - 修改已有记录时保存失败：同样退出码 2、标准输出为空，原文件逐字节保留，
//     本次新增内容不得落盘；
//   - 被业务拒绝但本应写入免修历史的首次申请，遇保存失败时不能显示“已记入
//     免修历史”，退出码 2（文件错误优先于业务拒绝码 1），历史中查不到该申请；
//   - 有效免修只有保存成功后才允许显示“有效”及获得学分的信息。

// makeDirUnwritable 取消目录的写权限并在测试结束时恢复。以 root 运行时权限
// 位无法阻止写入，此时跳过相关用例。
func makeDirUnwritable(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root 账户不受目录写权限限制，无法模拟保存失败")
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("取消目录写权限失败：%v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// assertSaveFailure 断言一次写入类命令在保存阶段失败：退出码 2、标准输出
// 没有任何业务内容，标准错误明确指出保存失败并点名目标记录文件。
func assertSaveFailure(t *testing.T, file string, args []string, forbidden []string, label string) {
	t.Helper()
	out, errText, code := runCLI(t, file, args...)
	if code != exitFile {
		t.Fatalf("%s：保存失败应退出码 %d，code=%d out=%q err=%q",
			label, exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("%s：保存未完成时标准输出不应包含业务结果，out=%q", label, out)
	}
	for _, want := range []string{"保存记录文件", file, "失败"} {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s：错误输出应说明保存失败并点名目标记录文件（缺 %q），err=%q",
				label, want, errText)
		}
	}
	for _, bad := range forbidden {
		if strings.Contains(errText, bad) || strings.Contains(out, bad) {
			t.Fatalf("%s：保存失败时不得出现 %q，out=%q err=%q", label, bad, out, errText)
		}
	}
}

// TestCLISaveFailureMissingParentHidesBusinessResult 目标记录文件与父目录都
// 不存在时登记学生：保存失败必须以退出码 2 结束，标准输出保持为空，不能先
// 显示“已登记学生”，也不能谎称已创建记录文件。
func TestCLISaveFailureMissingParentHidesBusinessResult(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing-parent", "records.json")

	assertSaveFailure(t, file, []string{"student", "s1"},
		[]string{"已登记学生", "已从空记录开始并创建", "从空记录"}, "首次登记学生")

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("保存失败后不得创建记录文件，stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Dir(file)); !os.IsNotExist(err) {
		t.Fatalf("保存失败后不得创建父目录，stat err=%v", err)
	}
}

// TestCLISaveFailureKeepsExistingFileUnchanged 修改已有记录时保存失败：退出码
// 2、标准输出为空，原文件逐字节保留，本次新增的学生不得落盘。
func TestCLISaveFailureKeepsExistingFileUnchanged(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")
	if _, _, code := runCLI(t, file, "student", "s1"); code != 0 {
		t.Fatal("准备初始记录失败")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	makeDirUnwritable(t, dir)
	assertSaveFailure(t, file, []string{"student", "s2"},
		[]string{"已登记学生"}, "向已有记录追加学生")

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("原文件应继续可读：%v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("保存失败后原文件必须逐字节保留\nwant=%q\n got=%q", raw, got)
	}
	if strings.Contains(string(got), "s2") {
		t.Fatalf("未保存成功的学生 s2 不得落入文件，content=%q", got)
	}
}

// TestCLISaveFailureRejectedWaiverReportsFileErrorNotHistory 因目标要求不存在
// 而被业务拒绝的首次申请本应写入免修历史；若这份历史没有保存成功，必须报告
// 文件错误并退出码 2，不能显示“已记入免修历史”，业务拒绝码 1 不能掩盖
// 保存失败，恢复写入后历史中也查不到这份申请。
func TestCLISaveFailureRejectedWaiverReportsFileErrorNotHistory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")
	if _, _, code := runCLI(t, file, "student", "s1"); code != 0 {
		t.Fatal("准备初始记录失败")
	}

	makeDirUnwritable(t, dir)
	assertSaveFailure(t, file, []string{"waiver", "s1", "rX", "wbad", "依据材料"},
		[]string{"已拒绝", "已记入学生", "免修历史"}, "被拒绝免修保存失败")

	// 恢复写入后重新打开进程：未保存成功的申请不得出现在历史中。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 应成功，code=%d out=%q", code, show)
	}
	if strings.Contains(show, "wbad") {
		t.Fatalf("保存失败的被拒绝申请不得留在免修历史中，out=%q", show)
	}
}

// TestCLISaveFailureApprovedWaiverHidesApproved 本应有效的免修申请只有保存
// 成功后才能显示“有效”及获得学分的信息；保存失败时退出码 2、标准输出为空，
// 恢复后文件中不存在这份免修。
func TestCLISaveFailureApprovedWaiverHidesApproved(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "数学", "4"},
		{"req", "s1", "r1", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("准备步骤 %v 失败，err=%q", st, errText)
		}
	}

	makeDirUnwritable(t, dir)
	assertSaveFailure(t, file, []string{"waiver", "s1", "r1", "w1", "竞赛获奖"},
		[]string{"有效", "获得课程学分"}, "有效免修保存失败")

	show, _, _ := runCLI(t, file, "show", "s1")
	if strings.Contains(show, "w1") {
		t.Fatalf("保存失败的免修不得落入历史，out=%q", show)
	}
	check, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(check, "总学分：0") ||
		strings.Contains(check, "来源为有效免修 w1") {
		t.Fatalf("未保存的免修不能成为学分来源，code=%d out=%q", code, check)
	}
}

// TestCLISaveFailureForResultSubmitAndRevoke 提交结果与撤销免修在保存失败时
// 同样不得宣布完成：退出码 2、标准输出为空，文件中的状态保持提交/撤销之前。
func TestCLISaveFailureForResultSubmitAndRevoke(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"waiver", "s1", "r1", "w1", "依据材料"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("准备步骤 %v 失败，err=%q", st, errText)
		}
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	makeDirUnwritable(t, dir)
	assertSaveFailure(t, file, []string{"pass", "s1", "e1"},
		[]string{"结果已提交", "通过"}, "提交通过保存失败")
	assertSaveFailure(t, file, []string{"revoke-waiver", "s1", "w1", "材料无法核实"},
		[]string{"已撤销"}, "撤销免修保存失败")

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("两次保存失败后原文件必须逐字节保留\nwant=%q\n got=%q", raw, got)
	}
}

// TestCLISaveSuccessShowsBusinessThenCreateHint 正常保存成功时业务输出内容与
// 先后顺序不变：业务结果在前，首次创建记录文件的说明在最后；再次修改已有
// 文件时不再出现创建说明。
func TestCLISaveSuccessShowsBusinessThenCreateHint(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")

	out, _, code := runCLI(t, file, "student", "s1")
	if code != 0 {
		t.Fatalf("首次登记应成功，code=%d out=%q", code, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 ||
		!strings.Contains(lines[0], "已登记学生 s1") ||
		!strings.Contains(lines[1], "已从空记录开始并创建") {
		t.Fatalf("业务结果应在前、首次创建说明在后，out=%q", out)
	}

	out, _, code = runCLI(t, file, "course", "c1", "数学", "4")
	if code != 0 || !strings.Contains(out, "已登记课程 c1") ||
		strings.Contains(out, "从空记录") {
		t.Fatalf("修改已有文件应只输出业务结果、不再出现创建说明，code=%d out=%q", code, out)
	}
}
