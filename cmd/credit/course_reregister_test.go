package main

import (
	"os"
	"strings"
	"testing"
)

// 本文件围绕“用 course 以原编号重新登记已有课程”的行为做命令行回归：
// 每次调用都重新打开记录文件，因此下列资料与核对检查同时覆盖落盘后再加载的结果。
//   - 课程未被任何课程要求引用：可用新名称与新的正整数学分原地更新，
//     开放/停开状态不因重新登记改变，课程列表仍只有这一份记录；
//   - 一旦被任意学生的课程要求引用（建立要求即生效，无需选课或通过）：
//     学分锁定，连同新名称一起的整次更新必须整单拒绝（退出码 1），
//     名称、学分、状态与记录文件都保持原样；
//   - 被引用后提交与原学分相同的新名称：允许只改名，编号与学分不变，
//     已有修读、免修、核对结论、学分来源与总学分都不受影响，停开仍停开；
//   - 未被引用时零或负数学分同样拒绝，且不能顺带改掉名称或状态。

// mustReadRecord 读取记录文件当前内容，供拒绝前后逐字节比对。
func mustReadRecord(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("读取记录文件失败：%v", err)
	}
	return string(b)
}

// assertRecordUnchanged 断言一次被拒绝的请求没有改写记录文件。
func assertRecordUnchanged(t *testing.T, file, before, label string) {
	t.Helper()
	if got := mustReadRecord(t, file); got != before {
		t.Fatalf("%s不应改写记录文件\nbefore=%s\nafter=%s", label, before, got)
	}
}

// assertSingleCourseLine 断言课程列表中该编号只有一份记录，且整行与预期一致。
func assertSingleCourseLine(t *testing.T, file, wantLine string, courseID string) {
	t.Helper()
	out, _, code := runCLI(t, file, "list-courses")
	if code != 0 {
		t.Fatalf("列出课程失败 code=%d out=%q", code, out)
	}
	if strings.Count(out, "课程 "+courseID+"《") != 1 {
		t.Fatalf("课程 %s 应只保留一份记录，实际列表：%q", courseID, out)
	}
	if !strings.Contains(out, wantLine) {
		t.Fatalf("课程列表应包含 %q，实际：%q", wantLine, out)
	}
}

// TestCLICourseReregisterBeforeReferenceUpdatesInPlace 未被任何要求引用的开放
// 课程用原编号提交新名称与新的正整数学分：原地更新、列表只剩一份、状态继续
// 开放；此后建立的要求与修读核对都使用更新后的名称与学分。
func TestCLICourseReregisterBeforeReferenceUpdatesInPlace(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 新名称 + 新正整数学分：更新成功（退出码 0），不能被当成业务拒绝。
	out, errText, code := runCLI(t, file, "course", "c1", "高等数学（上）", "5")
	if code != 0 || !strings.Contains(out, "已更新") {
		t.Fatalf("未被引用的课程应允许更新，code=%d out=%q err=%q", code, out, errText)
	}

	// 列表只剩这一份记录，显示新名称、新学分，且仍为开放。
	assertSingleCourseLine(t, file, "课程 c1《高等数学（上）》5 学分，状态：开放", "c1")
	if out, _, _ := runCLI(t, file, "list-courses"); strings.Contains(out, "高等数学》") {
		t.Fatalf("更新后不应再显示旧名称，out=%q", out)
	}

	// 之后建立要求、选课并通过：核对必须使用更新后的名称与 5 学分。
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
	if code != 0 || !strings.Contains(out, "总学分：5") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学（上）》，5 学分）：已满足") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("更新后的名称与 5 学分应进入核对，code=%d out=%q", code, out)
	}
	out, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(out, "要求 r1 -> 课程 c1《高等数学（上）》5 学分") {
		t.Fatalf("要求查询应显示更新后的课程资料，out=%q", out)
	}
}

// TestCLICourseReregisterClosedCourseKeepsClosed 已停开的课程在未被引用时
// 允许用原编号更新名称与学分，但更新后必须仍为停开，不能借重新登记恢复开放；
// 原有开放的课程更新后继续开放（见上一用例）。
func TestCLICourseReregisterClosedCourseKeepsClosed(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"course", "c1", "高等数学", "4"},
		{"course-close", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	out, _, code := runCLI(t, file, "course", "c1", "高等数学（下）", "6")
	if code != 0 || !strings.Contains(out, "已更新") {
		t.Fatalf("停开但未被引用的课程也应允许更新，code=%d out=%q", code, out)
	}
	assertSingleCourseLine(t, file, "课程 c1《高等数学（下）》6 学分，状态：停开", "c1")

	// 更新后仍停开：可以建立要求，但不能新增修读。
	if _, errText, code := runCLI(t, file, "student", "s1"); code != 0 {
		t.Fatalf("登记学生失败 code=%d err=%q", code, errText)
	}
	if _, errText, code := runCLI(t, file, "req", "s1", "r1", "c1"); code != 0 {
		t.Fatalf("停开课程应仍可建立要求，code=%d err=%q", code, errText)
	}
	if _, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", "e1"); code != 1 {
		t.Fatalf("更新后仍停开的课程不能新增修读，code=%d err=%q", code, errText)
	}
}

// TestCLICourseReregisterReferencedCreditLocksWholeUpdate 课程一被课程要求
// 引用（只有要求、没有任何选课或通过结果）学分即锁定：同时提交新名称与不同
// 学分必须整单拒绝（退出码 1），错误说明指出已被要求引用与原学分；
// 原名称、原学分、开放状态与记录文件全部保留，不能只改掉名称。
// 课程停开不解除该限制；整单拒绝后，按原学分只改名称仍应成功。
func TestCLICourseReregisterReferencedCreditLocksWholeUpdate(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 只有要求、没有选课或通过结果：锁定已经生效。
	before := mustReadRecord(t, file)
	out, errText, code := runCLI(t, file, "course", "c1", "高等数学（下）", "5")
	if code != 1 {
		t.Fatalf("已被要求引用时改学分应整单拒绝并退出码 1，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(errText, "已被课程要求引用") ||
		!strings.Contains(errText, "原学分 4") {
		t.Fatalf("拒绝说明应指出已被要求引用及原学分 4，err=%q", errText)
	}
	assertRecordUnchanged(t, file, before, "被引用课程的整单更新")

	// 原名称、原学分、开放状态全部保留，只有一份记录。
	assertSingleCourseLine(t, file, "课程 c1《高等数学》4 学分，状态：开放", "c1")
	out, _, _ = runCLI(t, file, "check", "s1")
	if !strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：未满足") ||
		!strings.Contains(out, "总学分：0") {
		t.Fatalf("拒绝后要求核对仍应显示原名称与原学分，out=%q", out)
	}

	// 整单被拒后按原学分只改名称：应成功，证明拒绝没有破坏课程资料。
	if out, _, code := runCLI(t, file, "course", "c1", "高等数学（下）", "4"); code != 0 {
		t.Fatalf("相同学分只改名称应成功，code=%d out=%q", code, out)
	}
	assertSingleCourseLine(t, file, "课程 c1《高等数学（下）》4 学分，状态：开放", "c1")

	// 停开不解除学分锁定。
	if _, errText, code := runCLI(t, file, "course-close", "c1"); code != 0 {
		t.Fatalf("停开课程失败 code=%d err=%q", code, errText)
	}
	closed := mustReadRecord(t, file)
	if _, errText, code := runCLI(t, file, "course", "c1", "另一个名称", "5"); code != 1 {
		t.Fatalf("停开不应解除学分锁定，code=%d err=%q", code, errText)
	}
	if !strings.Contains(errText, "已被课程要求引用") || !strings.Contains(errText, "原学分 4") {
		t.Fatalf("停开后拒绝仍应说明被引用与原学分，err=%q", errText)
	}
	assertRecordUnchanged(t, file, closed, "停开课程的改学分请求")
	assertSingleCourseLine(t, file, "课程 c1《高等数学（下）》4 学分，状态：停开", "c1")
	out, _, _ = runCLI(t, file, "check", "s1")
	if !strings.Contains(out, "要求 r1（课程 c1《高等数学（下）》，4 学分）：未满足") {
		t.Fatalf("拒绝后名称与停开状态应保持，out=%q", out)
	}
}

// TestCLICourseReregisterReferencedRenameOnlyPreservesRequirementData
// 已被要求引用的课程提交新名称但学分与原值相同：允许只改名，课程编号与学分
// 不变；课程列表与已有要求查询显示新名称，已有修读与免修继续指向原要求，
// 核对的满足情况、学分来源和总学分不变；停开课程改名后仍然停开。
func TestCLICourseReregisterReferencedRenameOnlyPreservesRequirementData(t *testing.T) {
	file := tempRecordFile(t)
	// s1 以通过修读满足 r1；s2 以有效免修满足同号要求（同一门课程）。
	for _, st := range [][]string{
		{"student", "s1"},
		{"student", "s2"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
		{"req", "s2", "r1", "c1"},
		{"waiver", "s2", "r1", "w1", "外校同层次课程"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	// 改名前先停开，顺带回归“停开课程改名后仍然停开”。
	if _, errText, code := runCLI(t, file, "course-close", "c1"); code != 0 {
		t.Fatalf("停开课程失败 code=%d err=%q", code, errText)
	}

	// 新名称 + 与原值相同的学分：更新成功（退出码 0）。
	if out, errText, code := runCLI(t, file, "course", "c1", "高等数学（强化班）", "4"); code != 0 {
		t.Fatalf("被引用课程相同学分只改名应成功，code=%d out=%q err=%q", code, out, errText)
	}
	// 编号与学分不变、名称更新、仍为停开，且只有一份记录。
	assertSingleCourseLine(t, file, "课程 c1《高等数学（强化班）》4 学分，状态：停开", "c1")

	// 改名后仍停开：不能新增修读。
	if _, errText, code := runCLI(t, file, "enroll", "s2", "r1", "2025春", "e9"); code != 1 {
		t.Fatalf("改名后仍停开的课程不能新增修读，code=%d err=%q", code, errText)
	}

	// s1 的已有修读继续指向原要求，核对满足情况、来源与总学分不变，
	// 只在课程资料处显示新名称。
	out, _, code := runCLI(t, file, "show", "s1")
	if code != 0 ||
		!strings.Contains(out, "要求 r1 -> 课程 c1《高等数学（强化班）》4 学分") ||
		!strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：通过") {
		t.Fatalf("s1 的要求查询应显示新名称、已有通过修读不变，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学（强化班）》，4 学分）：已满足") ||
		!strings.Contains(out, "来源为通过修读 e1") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("s1 改名后仍应凭通过修读 e1 满足、总学分 4，out=%q", out)
	}

	// s2 的有效免修继续满足原要求，来源与总学分不变，课程显示新名称。
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学（强化班）》，4 学分）：已满足") ||
		!strings.Contains(out, "来源为有效免修 w1") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("s2 改名后仍应凭有效免修 w1 满足、总学分 4，out=%q", out)
	}
	out, _, _ = runCLI(t, file, "show", "s2")
	if !strings.Contains(out, "要求 r1 -> 课程 c1《高等数学（强化班）》4 学分") ||
		!strings.Contains(out, `免修 w1：要求 r1，依据 "外校同层次课程"，状态：有效`) {
		t.Fatalf("s2 的要求应显示新名称且免修记录原样保留，out=%q", out)
	}
}

// TestCLICourseReregisterRejectsNonPositiveCreditWithoutSideEffects 课程尚未
// 被引用时，零或负数学分同样必须拒绝；拒绝不能顺带改掉名称或开放/停开状态，
// 记录文件也不应变化。
func TestCLICourseReregisterRejectsNonPositiveCreditWithoutSideEffects(t *testing.T) {
	file := tempRecordFile(t)
	if _, errText, code := runCLI(t, file, "course", "c1", "高等数学", "4"); code != 0 {
		t.Fatalf("登记课程失败 code=%d err=%q", code, errText)
	}

	before := mustReadRecord(t, file)
	for _, credit := range []string{"0", "-3"} {
		out, errText, code := runCLI(t, file, "course", "c1", "错误名称", credit)
		if code != 1 || !strings.Contains(errText, "学分") {
			t.Fatalf("学分 %s 应作为业务规则拒绝（退出码 1），code=%d out=%q err=%q",
				credit, code, out, errText)
		}
		assertRecordUnchanged(t, file, before, "学分为 "+credit+" 的更新请求")
	}
	// 名称、学分、开放状态都保持原样，只有一份记录。
	assertSingleCourseLine(t, file, "课程 c1《高等数学》4 学分，状态：开放", "c1")

	// 停开状态同样不能被失败请求顺带改掉。
	if _, errText, code := runCLI(t, file, "course-close", "c1"); code != 0 {
		t.Fatalf("停开课程失败 code=%d err=%q", code, errText)
	}
	closed := mustReadRecord(t, file)
	if _, errText, code := runCLI(t, file, "course", "c1", "错误名称", "0"); code != 1 {
		t.Fatalf("停开课程的零学分请求应拒绝，code=%d err=%q", code, errText)
	}
	assertRecordUnchanged(t, file, closed, "停开课程的零学分请求")
	assertSingleCourseLine(t, file, "课程 c1《高等数学》4 学分，状态：停开", "c1")
}
