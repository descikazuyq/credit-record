package main

import (
	"os"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“恢复开放（course-open）”与新增修读、原有学分来源
// 之间的关系：恢复课程状态本身不代表某次修读通过，也不能把停开期间被拒绝的
// 登记变成已有记录。每条命令都重新打开记录文件，因此反馈、课程列表、学生
// 记录（show）与学分核对（check）之间的一致性同时覆盖落盘后再加载的结果：
//   - 停开期间用新修读编号登记被拒绝（退出码 1，说明课程停开），历史中没有
//     这份新修读；随后恢复开放成功（退出码 0），反馈与课程列表都显示开放，
//     课程编号、名称与学分保持原样；旧修读的要求、学期与结果保留，被拒绝的
//     新编号仍不存在，核对仍只获得原课程学分、来源仍是原先通过的旧修读；
//   - 恢复后重新提交先前被拒绝的登记可正常建立新修读（初始为选课），即使与
//     旧修读同学期、要求已有一次通过也不禁止登记；新增选课不增加学分、不
//     改变满足情况；新修读提交通过后两份通过历史都保留，要求仍只计一份
//     课程学分，并继续以最先提交通过结果的旧修读说明来源（不按新编号的
//     排列次序换用新修读）；提交结果后课程仍开放，未通过的旧修读不被连带
//     改变；
//   - 没有任何通过修读、也没有有效免修的要求：恢复开放后仍列为未满足，
//     总学分不会仅因恢复状态或新增选课而增长；
//   - 恢复不存在的课程以退出码 1 拒绝，错误说明点名课程不存在，不出现恢复
//     成功提示，不创建课程，也不改动已有记录。

// setupCLIReopenScenario 登记学生 s1 与 4 学分课程 c1，建立唯一要求 r1，
// 停开前登记两份修读：e1（2024春，已通过）与 e2（2024秋，未通过），
// 随后停开课程。返回时课程处于停开状态。
func setupCLIReopenScenario(t *testing.T, file string) {
	t.Helper()
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
		{"enroll", "s1", "r1", "2024秋", "e2"},
		{"fail", "s1", "e2"},
		{"course-close", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
}

// TestCLICourseReopenRestoresOpenWithoutTouchingHistory 停开期间的新登记被
// 拒绝后恢复开放：课程状态、编号、名称与学分恢复并原样展示，旧修读历史与
// 核对结论不变，被拒绝的新编号不会借恢复操作进入历史。
func TestCLICourseReopenRestoresOpenWithoutTouchingHistory(t *testing.T) {
	file := tempRecordFile(t)
	setupCLIReopenScenario(t, file)

	// 停开期间用新修读编号 e0 为同一要求登记修读：退出码 1，错误说明指出
	// 课程停开，不出现登记成功提示，记录文件不变。
	before := mustReadRecord(t, file)
	out, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2025春", "e0")
	if code != 1 {
		t.Fatalf("停开期间新增修读应拒绝且退出码 1，code=%d out=%q err=%q", code, out, errText)
	}
	if out != "" || strings.Contains(out, "已登记修读") {
		t.Fatalf("被拒绝时不应出现登记成功提示，out=%q", out)
	}
	if !strings.Contains(errText, "课程 c1 已停开，不能新增修读") {
		t.Fatalf("错误说明应指出课程 c1 停开，err=%q", errText)
	}
	assertRecordUnchanged(t, file, before, "停开期间新增修读被拒绝")

	// 恢复开放：退出码 0，反馈显示开放。
	out, errText, code = runCLI(t, file, "course-open", "c1")
	if code != 0 || !strings.Contains(out, "课程 c1《高等数学》状态：开放") {
		t.Fatalf("恢复开放应成功并显示开放，code=%d out=%q err=%q", code, out, errText)
	}

	// 课程列表同样显示开放，编号、名称与 4 学分保持原样，且只有一份记录。
	assertSingleCourseLine(t, file, "课程 c1《高等数学》4 学分，状态：开放", "c1")

	// 学生历史：旧修读的要求、学期与结果保留（e1 通过、e2 未通过），
	// 先前被拒绝的新编号 e0 仍不存在——恢复开放不能把它变成已有记录。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：通过") ||
		!strings.Contains(out, "修读 e2：要求 r1，学期 2024秋，结果：未通过") {
		t.Fatalf("旧修读的要求、学期与结果应原样保留，out=%q", out)
	}
	if strings.Contains(out, "修读 e0") {
		t.Fatalf("被拒绝的新修读 e0 不应出现在历史中，out=%q", out)
	}

	// 核对：仍只获得 4 学分，来源仍是原先通过的旧修读 e1；
	// 恢复课程状态本身不代表某次修读通过。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：已满足，来源为通过修读 e1") ||
		strings.Contains(out, "未满足要求：[") ||
		strings.Contains(out, "共通过 2 次") {
		t.Fatalf("恢复后应仍仅由 e1 满足、计 4 学分，code=%d out=%q", code, out)
	}
}

// TestCLICourseReopenReenrollKeepsOriginalCreditSource 恢复开放后重新提交
// 先前被拒绝的登记：新修读正常建立（初始为选课），与旧修读同学期也不被
// 已有通过禁止；新修读提交通过后两份通过历史都保留，要求仍只计一份课程
// 学分，来源仍是最先提交通过结果的旧修读——新编号 e0 按字典序排在 e1
// 之前，若按编号排列次序选来源就会错用 e0。
func TestCLICourseReopenReenrollKeepsOriginalCreditSource(t *testing.T) {
	file := tempRecordFile(t)
	setupCLIReopenScenario(t, file)

	// 停开期间登记 e0 被拒绝，随后恢复开放。
	if _, _, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", "e0"); code != 1 {
		t.Fatalf("停开期间新增修读应拒绝且退出码 1，code=%d", code)
	}
	if _, errText, code := runCLI(t, file, "course-open", "c1"); code != 0 {
		t.Fatalf("恢复开放失败 code=%d err=%q", code, errText)
	}

	// 重新提交先前被拒绝的登记：与已通过修读 e1 处于同一学期（2024春），
	// 也不能因已有一次通过而禁止登记；新修读初始结果为选课。
	out, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", "e0")
	if code != 0 ||
		!strings.Contains(out, "已登记修读 e0：学生 s1，要求 r1，学期 2024春，状态：选课") {
		t.Fatalf("恢复后应能重新登记 e0 且初始为选课，code=%d out=%q err=%q", code, out, errText)
	}

	// 新增选课不增加学分、不改变要求的满足情况：仍 4 学分、来源仍是 e1。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "已满足，来源为通过修读 e1") ||
		strings.Contains(out, "共通过 2 次") {
		t.Fatalf("新增选课不应增加学分或改变满足情况，code=%d out=%q", code, out)
	}
	if out, _, _ := runCLI(t, file, "show", "s1"); !strings.Contains(out,
		"修读 e0：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("新修读 e0 应记录为选课，out=%q", out)
	}

	// 为新修读 e0 提交通过：退出码 0，反馈明确提交成功。
	out, errText, code = runCLI(t, file, "pass", "s1", "e0")
	if code != 0 || !strings.Contains(out, "修读 e0 结果已提交：通过") {
		t.Fatalf("应为新修读 e0 提交通过，code=%d out=%q err=%q", code, out, errText)
	}

	// 学生历史：两份通过修读都保留（e0 与 e1 均通过），未通过的旧修读
	// e2 不被连带改变。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"修读 e0：要求 r1，学期 2024春，结果：通过",
		"修读 e1：要求 r1，学期 2024春，结果：通过",
		"修读 e2：要求 r1，学期 2024秋，结果：未通过",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("历史中应保留 %q，out=%q", want, out)
		}
	}

	// 核对：两份通过仍只计一份课程学分（总学分 4，不是 8），并继续以最先
	// 提交通过结果的旧修读 e1 说明来源——不能因 e0 排列在前就换用新修读。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：已满足，来源为通过修读 e1（共通过 2 次，仅计一次学分）") ||
		strings.Contains(out, "来源为通过修读 e0") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("两次通过应仍只计 4 学分且来源仍是 e1，code=%d out=%q", code, out)
	}

	// 提交结果后课程仍为开放。
	assertSingleCourseLine(t, file, "课程 c1《高等数学》4 学分，状态：开放", "c1")
}

// TestCLICourseReopenUnmetRequirementStaysUnmet 没有任何通过修读、也没有
// 有效免修的要求：恢复开放后仍列为未满足，总学分不会仅因恢复状态或新增
// 选课而增长。
func TestCLICourseReopenUnmetRequirementStaysUnmet(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"course-close", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 恢复开放成功，课程列表显示开放。
	out, errText, code := runCLI(t, file, "course-open", "c1")
	if code != 0 || !strings.Contains(out, "课程 c1《高等数学》状态：开放") {
		t.Fatalf("恢复开放应成功，code=%d out=%q err=%q", code, out, errText)
	}
	assertSingleCourseLine(t, file, "课程 c1《高等数学》4 学分，状态：开放", "c1")

	// 恢复状态本身不带来学分：要求仍列为未满足，总学分仍为 0。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：未满足") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("恢复开放后无通过修读的要求应仍未满足、总学分 0，code=%d out=%q", code, out)
	}

	// 恢复后新增选课同样不带来学分：仍 0 学分、未满足。
	if _, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2025春", "e1"); code != 0 {
		t.Fatalf("恢复后应能新增修读，code=%d err=%q", code, errText)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("新增选课不应带来学分，code=%d out=%q", code, out)
	}
	if out, _, _ := runCLI(t, file, "show", "s1"); !strings.Contains(out,
		"修读 e1：要求 r1，学期 2025春，结果：选课") {
		t.Fatalf("新修读 e1 应记录为选课，out=%q", out)
	}
}

// TestCLICourseReopenUnknownCourseRejected 恢复不存在的课程：退出码 1，
// 错误说明点名课程不存在，不出现恢复成功提示，不创建课程，也不改动已有
// 记录；记录文件原本不存在时也不会凭空创建。
func TestCLICourseReopenUnknownCourseRejected(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 恢复从未登记的课程 c9：退出码 1，错误说明点名课程不存在，
	// 标准输出不出现恢复成功提示，记录文件不变。
	before := mustReadRecord(t, file)
	out, errText, code := runCLI(t, file, "course-open", "c9")
	if code != 1 {
		t.Fatalf("恢复不存在的课程应拒绝且退出码 1，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(errText, "课程 c9 不存在") {
		t.Fatalf("错误说明应点名课程 c9 不存在，err=%q", errText)
	}
	if strings.Contains(out, "状态：开放") || strings.Contains(out, "c9") {
		t.Fatalf("被拒绝时不应出现恢复成功提示，out=%q", out)
	}
	assertRecordUnchanged(t, file, before, "恢复不存在的课程被拒绝")

	// 不创建课程：课程列表仍只有 c1，已有记录（c1 资料、s1 的通过修读与
	// 学分）不受影响。
	assertSingleCourseLine(t, file, "课程 c1《高等数学》4 学分，状态：开放", "c1")
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("已有记录的学分与来源不应受影响，code=%d out=%q", code, out)
	}

	// 记录文件原本不存在时，恢复不存在的课程同样拒绝且不会凭空创建文件。
	ghost := tempRecordFile(t)
	out, errText, code = runCLI(t, ghost, "course-open", "c9")
	if code != 1 || !strings.Contains(errText, "课程 c9 不存在") {
		t.Fatalf("空记录上恢复不存在的课程应拒绝且退出码 1，code=%d out=%q err=%q",
			code, out, errText)
	}
	if strings.Contains(out, "状态：开放") {
		t.Fatalf("被拒绝时不应出现恢复成功提示，out=%q", out)
	}
	if _, err := os.Stat(ghost); !os.IsNotExist(err) {
		t.Fatalf("被拒绝的恢复不应凭空创建记录文件，stat err=%v", err)
	}
}
