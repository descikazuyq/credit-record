package main

import (
	"os"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“恢复开放（course-open）”这一现有功能：恢复课程
// 状态本身不代表某次修读通过，也不能把停开期间被拒绝的登记变成已有记录；
// 恢复后重新提交的新修读与原有通过记录在学分来源上的关系保持现有规则
// （同一要求多次通过只得一份课程学分，没有有效免修时以最先提交的通过记录
// 说明来源）。每条命令都重新打开记录文件，因此反馈、课程列表、学生记录
// （show）与学分核对（check）之间的一致性同时覆盖落盘后再加载的结果：
//   - 停开期间用新修读编号登记被拒绝（退出码 1，指出课程停开），恢复开放
//     成功（退出码 0）后课程编号、名称、学分保持原样，旧修读的要求、学期
//     与结果保留，被拒绝的新编号仍不存在，核对仍只获得原课程学分、来源仍
//     是原先通过的那份修读；
//   - 恢复后重新提交先前被拒绝的登记可正常建立新修读（初始为选课），即使
//     与旧修读同学期、要求已有一次通过也不禁止；新增选课不增加学分、不改
//     变满足情况；新修读再提交通过后两份通过历史都保留，仍只计一份学分，
//     来源仍是最先提交通过结果的旧修读，不因新编号的排列次序换用新修读；
//   - 没有任何通过修读、也没有有效免修的要求：恢复开放后仍列为未满足，
//     总学分不会仅因恢复状态或新增选课而增长；
//   - 恢复不存在的课程以退出码 1 拒绝，错误说明点名课程不存在，不出现
//     恢复成功提示，不创建课程，也不改动已有记录。

// TestCLIReopenCourseRestoresOpenAndKeepsHistoryAndCredits 停开期间的新登记
// 被拒绝后，恢复开放只改变课程状态：课程资料原样保留，旧修读历史原样保留，
// 被拒绝的新编号不出现，核对学分与来源不变。
func TestCLIReopenCourseRestoresOpenAndKeepsHistoryAndCredits(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"enroll", "s1", "r1", "2024秋", "e2"},
		{"pass", "s1", "e1"},
		{"fail", "s1", "e2"},
		{"course-close", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 停开期间用新修读编号 e3 为同一要求登记修读：退出码 1，错误说明指出
	// 课程停开，不出现登记成功提示，记录文件不变。
	before := mustReadRecord(t, file)
	out, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2025春", "e3")
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
	if code != 0 {
		t.Fatalf("恢复开放应成功，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(out, "课程 c1《高等数学》状态：开放") {
		t.Fatalf("恢复开放反馈应显示课程状态为开放，out=%q", out)
	}

	// 课程列表显示开放，课程编号、名称与 4 学分保持原样，且只有一份记录。
	assertSingleCourseLine(t, file, "课程 c1《高等数学》4 学分，状态：开放", "c1")

	// 学生历史：旧修读的要求、学期与结果保留（e1 通过、e2 未通过），
	// 停开期间被拒绝的新编号 e3 仍不存在——恢复开放不能把被拒绝的登记
	// 变成已有记录。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：通过") ||
		!strings.Contains(out, "修读 e2：要求 r1，学期 2024秋，结果：未通过") {
		t.Fatalf("旧修读的要求、学期与结果应原样保留，out=%q", out)
	}
	if strings.Contains(out, "修读 e3") {
		t.Fatalf("被拒绝的新修读 e3 不应因恢复开放进入历史，out=%q", out)
	}

	// 核对仍只获得 4 学分，来源仍是原先通过的旧修读 e1：恢复课程状态本身
	// 不代表某次修读通过，未通过的 e2 不计学分。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：已满足，来源为通过修读 e1") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("恢复开放后应仍凭 e1 获得 4 学分，code=%d out=%q", code, out)
	}
}

// TestCLIReopenThenResubmitRejectedEnrollmentKeepsOriginalCreditSource 恢复开放
// 后重新提交先前被拒绝的登记：新修读正常建立、初始为选课，不因同一要求已有
// 一次通过而被禁止；新修读再提交通过后，两份通过历史都保留，但该要求仍只
// 获得一份课程学分，来源继续以最先提交通过结果的旧修读说明——新编号 e0
// 按字典序排在 e1 之前，若按编号排列次序取来源就会错换成 e0。
func TestCLIReopenThenResubmitRejectedEnrollmentKeepsOriginalCreditSource(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"enroll", "s1", "r1", "2024秋", "e2"},
		{"pass", "s1", "e1"},
		{"fail", "s1", "e2"},
		{"course-close", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 停开期间以新编号 e0 登记被拒绝；恢复开放后重新提交同一编号。
	if _, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", "e0"); code != 1 {
		t.Fatalf("停开期间新增修读应拒绝且退出码 1，code=%d err=%q", code, errText)
	}
	if _, errText, code := runCLI(t, file, "course-open", "c1"); code != 0 {
		t.Fatalf("恢复开放应成功，code=%d err=%q", code, errText)
	}

	// 重新提交先前被拒绝的登记：与旧修读 e1 处于同一学期（2024春），且
	// 该要求已有一次通过，仍应正常建立新修读，初始结果为选课。
	out, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", "e0")
	if code != 0 {
		t.Fatalf("恢复开放后重新提交先前被拒绝的登记应成功，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(out, "已登记修读 e0：学生 s1，要求 r1，学期 2024春，状态：选课") {
		t.Fatalf("新修读 e0 应登记成功且初始为选课，out=%q", out)
	}

	// 新增选课不增加学分、不改变要求的满足情况：仍 4 学分、来源仍是 e1。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "已满足，来源为通过修读 e1") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("新增选课不应改变学分与满足情况，code=%d out=%q", code, out)
	}
	if out, _, _ := runCLI(t, file, "show", "s1"); !strings.Contains(out,
		"修读 e0：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("新修读 e0 应保持选课，out=%q", out)
	}

	// 为新修读 e0 提交通过：退出码 0。
	out, errText, code = runCLI(t, file, "pass", "s1", "e0")
	if code != 0 || !strings.Contains(out, "修读 e0 结果已提交：通过") {
		t.Fatalf("新修读 e0 提交通过应成功，code=%d out=%q err=%q", code, out, errText)
	}

	// 两份通过历史都保留：e1 与 e0 均为通过；未通过的旧修读 e2 不被连带改变。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 ||
		!strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：通过") ||
		!strings.Contains(out, "修读 e0：要求 r1，学期 2024春，结果：通过") ||
		!strings.Contains(out, "修读 e2：要求 r1，学期 2024秋，结果：未通过") {
		t.Fatalf("两份通过历史应保留且 e2 仍为未通过，code=%d out=%q", code, out)
	}

	// 该要求仍只获得一份课程学分（4 学分），来源继续以最先提交通过结果的
	// 旧修读 e1 说明：不能因新编号 e0 的排列次序而换用新修读。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：已满足，来源为通过修读 e1") ||
		!strings.Contains(out, "（共通过 2 次，仅计一次学分）") ||
		strings.Contains(out, "来源为通过修读 e0") {
		t.Fatalf("两次通过仍只计 4 学分且来源应保持为最先提交的 e1，out=%q", out)
	}

	// 提交结果后课程仍为开放。
	assertSingleCourseLine(t, file, "课程 c1《高等数学》4 学分，状态：开放", "c1")
}

// TestCLIReopenKeepsUnmetRequirementAndZeroCredits 没有任何通过修读、也没有
// 有效免修的要求：恢复开放后仍列为未满足，总学分不会仅因恢复状态或新增
// 选课而增长。
func TestCLIReopenKeepsUnmetRequirementAndZeroCredits(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"fail", "s1", "e1"},
		{"course-close", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 恢复开放成功，课程列表显示开放。
	out, errText, code := runCLI(t, file, "course-open", "c1")
	if code != 0 || !strings.Contains(out, "课程 c1《高等数学》状态：开放") {
		t.Fatalf("恢复开放应成功并显示开放，code=%d out=%q err=%q", code, out, errText)
	}
	assertSingleCourseLine(t, file, "课程 c1《高等数学》4 学分，状态：开放", "c1")

	// 恢复状态本身不带来学分：要求仍列为未满足，总学分仍为 0。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：未满足") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("恢复开放后要求应仍未满足、总学分 0，code=%d out=%q", code, out)
	}

	// 恢复后可以新增修读，但新增选课同样不增长学分、不改变满足情况。
	out, errText, code = runCLI(t, file, "enroll", "s1", "r1", "2025春", "e2")
	if code != 0 || !strings.Contains(out, "状态：选课") {
		t.Fatalf("恢复开放后新增修读应成功且初始为选课，code=%d out=%q err=%q",
			code, out, errText)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("新增选课不应使总学分增长或改变满足情况，code=%d out=%q", code, out)
	}
	if out, _, _ := runCLI(t, file, "show", "s1"); !strings.Contains(out,
		"修读 e1：要求 r1，学期 2024春，结果：未通过") ||
		!strings.Contains(out, "修读 e2：要求 r1，学期 2025春，结果：选课") {
		t.Fatalf("新旧修读历史应原样保留，out=%q", out)
	}
}

// TestCLIReopenNonexistentCourseRejected 恢复不存在的课程：退出码 1，错误
// 说明点名课程不存在，不出现恢复成功提示，不创建课程，也不改动已有记录。
func TestCLIReopenNonexistentCourseRejected(t *testing.T) {
	// 已有记录文件：恢复另一个不存在的编号，已有课程与记录原样保留。
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
		{"course-close", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	before := mustReadRecord(t, file)
	out, errText, code := runCLI(t, file, "course-open", "cX")
	if code != 1 {
		t.Fatalf("恢复不存在的课程应拒绝且退出码 1，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(errText, "课程 cX 不存在") {
		t.Fatalf("错误说明应点名课程 cX 不存在，err=%q", errText)
	}
	if strings.Contains(out, "状态：开放") || strings.Contains(out, "课程 cX") {
		t.Fatalf("被拒绝时不应出现恢复成功提示，out=%q", out)
	}
	assertRecordUnchanged(t, file, before, "恢复不存在的课程被拒绝")

	// 不创建课程：列表中只有原有的 c1，且其停开状态不受影响。
	assertSingleCourseLine(t, file, "课程 c1《高等数学》4 学分，状态：停开", "c1")
	if out, _, _ := runCLI(t, file, "list-courses"); strings.Contains(out, "cX") {
		t.Fatalf("被拒绝的恢复不应创建课程 cX，out=%q", out)
	}
	// 已有记录不受影响：s1 的学分与来源保持原样。
	if out, _, _ := runCLI(t, file, "check", "s1"); !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("已有学生的核对结果不应受影响，out=%q", out)
	}

	// 记录文件尚不存在时：恢复不存在的课程同样退出码 1，且不会凭空创建文件。
	fresh := tempRecordFile(t)
	out, errText, code = runCLI(t, fresh, "course-open", "cX")
	if code != 1 || !strings.Contains(errText, "课程 cX 不存在") {
		t.Fatalf("无记录文件时恢复不存在的课程应拒绝且退出码 1，code=%d out=%q err=%q",
			code, out, errText)
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Fatalf("被拒绝的恢复不应创建记录文件，stat err=%v", err)
	}
}
