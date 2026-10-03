package main

import (
	"os"
	"strings"
	"testing"
)

// 本文件通过命令行入口（每次调用都重新加载并保存记录文件）回归“用 course
// 重新登记已有课程编号”的行为，覆盖操作后的课程资料（list-courses/show）、
// 学生核对（check）以及记录文件落盘结果。

// countLines 返回 out 中以 prefix 开头的行数。
func countLines(out, prefix string) int {
	n := 0
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

// TestCLICourseReregisterBeforeReferenceUpdatesProfile 课程尚未被引用时，
// 用原编号提交新名称和新的正整数学分应原地更新：课程列表只保留一份记录，
// 显示新名称与新学分，原来开放的继续开放；之后建立的要求使用更新后的资料。
func TestCLICourseReregisterBeforeReferenceUpdatesProfile(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "数学", "4"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 用原编号提交新名称、新学分：成功（退出码 0），提示状态保持开放。
	out, _, code := runCLI(t, file, "course", "c1", "高等数学", "5")
	if code != 0 || !strings.Contains(out, "已更新") ||
		!strings.Contains(out, "《高等数学》5 学分") || !strings.Contains(out, "开放") {
		t.Fatalf("未引用课程重新登记应成功并显示新资料、保持开放，code=%d out=%q", code, out)
	}

	// 列表里只保留 c1 这一份记录，且为新名称、新学分、开放。
	out, _, code = runCLI(t, file, "list-courses")
	if code != 0 {
		t.Fatalf("list-courses 失败：%s", out)
	}
	if n := countLines(out, "课程 c1《"); n != 1 {
		t.Fatalf("课程列表应只保留一份 c1 记录，得到 %d 行：%q", n, out)
	}
	if !strings.Contains(out, "课程 c1《高等数学》5 学分，状态：开放") {
		t.Fatalf("列表应显示新名称、新学分与开放状态，out=%q", out)
	}
	if strings.Contains(out, "《数学》") {
		t.Fatalf("列表不应再显示旧名称，out=%q", out)
	}

	// 更新后才建立要求、选课并通过：核对使用更新后的名称与学分 5。
	for _, st := range [][]string{
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败：%s", out)
	}
	if !strings.Contains(out, "总学分：5") {
		t.Fatalf("后续要求应按新学分 5 计，out=%q", out)
	}
	if !strings.Contains(out, "课程 c1《高等数学》，5 学分") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("核对结果应显示更新后的课程资料与来源，out=%q", out)
	}
}

// TestCLICourseReregisterClosedBeforeReferenceStaysClosed 已停开但尚未被
// 引用的课程也允许更新，但重新登记不能让它恢复开放；更新后仍为停开，
// 不能新增修读，要求核对显示更新后的名称与学分。
func TestCLICourseReregisterClosedBeforeReferenceStaysClosed(t *testing.T) {
	file := tempRecordFile(t)
	runCLI(t, file, "student", "s1")
	runCLI(t, file, "course", "c1", "数学", "4")
	if _, errText, code := runCLI(t, file, "course-close", "c1"); code != 0 {
		t.Fatalf("停开课程失败，code=%d err=%q", code, errText)
	}

	out, _, code := runCLI(t, file, "course", "c1", "高等数学", "5")
	if code != 0 || !strings.Contains(out, "已更新") ||
		!strings.Contains(out, "《高等数学》5 学分") || !strings.Contains(out, "停开") {
		t.Fatalf("停开课程重新登记应成功但状态保持停开，code=%d out=%q", code, out)
	}
	out, _, _ = runCLI(t, file, "list-courses")
	if !strings.Contains(out, "课程 c1《高等数学》5 学分，状态：停开") {
		t.Fatalf("列表应显示新资料且仍为停开，out=%q", out)
	}

	// 对更新后的课程建立要求使用新资料；课程仍停开，不能新增修读。
	if _, errText, code := runCLI(t, file, "req", "s1", "r1", "c1"); code != 0 {
		t.Fatalf("更新后建立要求应成功，code=%d err=%q", code, errText)
	}
	if _, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", "e1"); code == 0 ||
		!strings.Contains(errText, "停开") {
		t.Fatalf("更新后仍停开的课程应拒绝新增修读，code=%d err=%q", code, errText)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "课程 c1《高等数学》，5 学分") ||
		!strings.Contains(out, "总学分：0") || !strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("未满足要求的核对应显示更新后资料、0 学分，code=%d out=%q", code, out)
	}
}

// TestCLICourseReregisterReferencedCreditChangeRejected 一旦课程被任意
// 学生的课程要求引用（无需选课或通过结果），同时改名并改学分应整次拒绝：
// 退出码 1，错误说明指出已被要求引用及原学分；原名称、原学分、开放状态
// 与记录文件内容全部保留，不能出现学分没改但名称已改掉的部分更新。
// 停开不会解除该限制。
func TestCLICourseReregisterReferencedCreditChangeRejected(t *testing.T) {
	file := tempRecordFile(t)
	runCLI(t, file, "student", "s1")
	runCLI(t, file, "course", "c1", "数学", "4")
	if _, errText, code := runCLI(t, file, "req", "s1", "r1", "c1"); code != 0 {
		t.Fatalf("建立要求应成功，code=%d err=%q", code, errText)
	}

	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// 只建立了要求、没有任何选课或通过结果：同时提交新名称与不同学分仍拒绝。
	out, errText, code := runCLI(t, file, "course", "c1", "高等数学", "5")
	if code != 1 {
		t.Fatalf("已被要求引用的课程改学分应拒绝且退出码 1，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(errText, "引用") || !strings.Contains(errText, "原学分 4") {
		t.Fatalf("错误说明应指出已被要求引用及原学分 4，err=%q", errText)
	}
	if strings.Contains(out, "已更新") {
		t.Fatalf("被拒绝时不应在标准输出报告更新成功，out=%q", out)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("被拒绝的更新不应改动记录文件\nbefore=%s\nafter=%s", before, after)
	}

	// 原名称、原学分、开放状态全部保留。
	list, _, _ := runCLI(t, file, "list-courses")
	if !strings.Contains(list, "课程 c1《数学》4 学分，状态：开放") ||
		strings.Contains(list, "高等数学") {
		t.Fatalf("拒绝后课程应保持原名称、原学分、开放，list=%q", list)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "课程 c1《数学》，4 学分") ||
		!strings.Contains(out, "总学分：0") || !strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("拒绝后学生核对应保持原样，code=%d out=%q", code, out)
	}

	// 原内容重复提交仍是幂等成功（退出码 0），不改动资料。
	if out, _, code := runCLI(t, file, "course", "c1", "数学", "4"); code != 0 ||
		!strings.Contains(out, "内容一致") {
		t.Fatalf("原内容重复提交应幂等成功，code=%d out=%q", code, out)
	}

	// 停开课程不会解除限制：停开后再次改名并改学分，仍拒绝且仍为停开。
	if _, errText, code := runCLI(t, file, "course-close", "c1"); code != 0 {
		t.Fatalf("停开课程失败，code=%d err=%q", code, errText)
	}
	before, err = os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, errText, code := runCLI(t, file, "course", "c1", "高等数学", "6"); code != 1 ||
		!strings.Contains(errText, "引用") || !strings.Contains(errText, "原学分 4") {
		t.Fatalf("停开不应解除学分保护，code=%d err=%q", code, errText)
	}
	after, err = os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("停开后被拒绝的更新仍不应改动文件\nbefore=%s\nafter=%s", before, after)
	}
	list, _, _ = runCLI(t, file, "list-courses")
	if !strings.Contains(list, "课程 c1《数学》4 学分，状态：停开") {
		t.Fatalf("拒绝后应保持原名称、原学分、停开，list=%q", list)
	}
}

// TestCLICourseReregisterReferencedRenameSameCreditAllowed 已被引用的课程
// 提交与原值相同的学分时允许只改名称：编号与学分不变，课程列表与已有学生的
// 要求查询显示新名称；已有修读与免修继续指向原要求，核对的满足情况、学分
// 来源与总学分不因改名改变。
func TestCLICourseReregisterReferencedRenameSameCreditAllowed(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "旧名称", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
		{"waiver", "s1", "r1", "w1", "学科竞赛获奖"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 只改名称、学分仍为 4：成功（退出码 0）。
	out, _, code := runCLI(t, file, "course", "c1", "高等数学", "4")
	if code != 0 || !strings.Contains(out, "已更新") ||
		!strings.Contains(out, "《高等数学》4 学分") || !strings.Contains(out, "开放") {
		t.Fatalf("相同学分的改名应成功，code=%d out=%q", code, out)
	}

	// 列表只有一份 c1 记录：编号、学分不变，名称更新。
	out, _, _ = runCLI(t, file, "list-courses")
	if n := countLines(out, "课程 c1《"); n != 1 {
		t.Fatalf("改名后课程仍应只有一份，得到 %d 行：%q", n, out)
	}
	if !strings.Contains(out, "课程 c1《高等数学》4 学分，状态：开放") ||
		strings.Contains(out, "旧名称") {
		t.Fatalf("列表应显示新名称与原学分，out=%q", out)
	}

	// 学生的要求查询显示新名称；修读与免修继续指向原要求 r1。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败：%s", out)
	}
	if !strings.Contains(out, "要求 r1 -> 课程 c1《高等数学》4 学分") {
		t.Fatalf("要求查询应显示新名称与原学分，out=%q", out)
	}
	if !strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：通过") {
		t.Fatalf("已有修读应继续指向原要求 r1，out=%q", out)
	}
	if !strings.Contains(out, "免修 w1：要求 r1") || !strings.Contains(out, "状态：有效") {
		t.Fatalf("已有免修应继续指向原要求 r1 且仍有效，out=%q", out)
	}

	// 核对：满足情况、来源（有效免修 w1）、通过历史 e1 与总学分 4 均不变，
	// 只呈现新名称。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败：%s", out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "课程 c1《高等数学》，4 学分") ||
		!strings.Contains(out, "来源为有效免修 w1") ||
		!strings.Contains(out, "通过修读历史 [e1]") {
		t.Fatalf("改名后满足情况、来源与总学分应不变且显示新名称，out=%q", out)
	}
	if strings.Contains(out, "未满足要求：[") || strings.Contains(out, "旧名称") {
		t.Fatalf("改名不应产生未满足要求或残留旧名称，out=%q", out)
	}
}

// TestCLICourseReregisterReferencedRenameKeepsClosed 停开课程被引用后，
// 以原学分改名仍应成功，但改名后课程保持停开、不能新增修读。
func TestCLICourseReregisterReferencedRenameKeepsClosed(t *testing.T) {
	file := tempRecordFile(t)
	runCLI(t, file, "student", "s1")
	runCLI(t, file, "course", "c1", "旧名称", "4")
	runCLI(t, file, "req", "s1", "r1", "c1")
	if _, errText, code := runCLI(t, file, "course-close", "c1"); code != 0 {
		t.Fatalf("停开课程失败，code=%d err=%q", code, errText)
	}

	if out, _, code := runCLI(t, file, "course", "c1", "高等数学", "4"); code != 0 ||
		!strings.Contains(out, "已更新") {
		t.Fatalf("停开课程以原学分改名应成功，code=%d out=%q", code, out)
	}
	out, _, _ := runCLI(t, file, "list-courses")
	if !strings.Contains(out, "课程 c1《高等数学》4 学分，状态：停开") {
		t.Fatalf("改名后应显示新名称且仍为停开，out=%q", out)
	}
	if _, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", "e1"); code == 0 ||
		!strings.Contains(errText, "停开") {
		t.Fatalf("改名后课程仍停开，应拒绝新增修读，code=%d err=%q", code, errText)
	}
}

// TestCLICourseReregisterNonPositiveCreditRejected 课程尚未被引用时，
// 零或负数学分也应拒绝（退出码 1），不能顺带改掉名称或开放状态，记录
// 文件内容不变。
func TestCLICourseReregisterNonPositiveCreditRejected(t *testing.T) {
	file := tempRecordFile(t)
	runCLI(t, file, "course", "c1", "数学", "4")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, credit := range []string{"0", "-3"} {
		out, errText, code := runCLI(t, file, "course", "c1", "高等数学", credit)
		if code != 1 || !strings.Contains(errText, "正整数") {
			t.Fatalf("学分 %s 应按业务规则拒绝（退出码 1），code=%d out=%q err=%q",
				credit, code, out, errText)
		}
		after, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatalf("学分 %s 被拒绝不应改动记录文件", credit)
		}
		list, _, _ := runCLI(t, file, "list-courses")
		if !strings.Contains(list, "课程 c1《数学》4 学分，状态：开放") ||
			strings.Contains(list, "高等数学") {
			t.Fatalf("学分 %s 被拒绝后名称与状态应保持原样，list=%q", credit, list)
		}
	}
}
