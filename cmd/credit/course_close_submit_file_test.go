package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“课程停开只限制新增修读”这一现有规则：
// 停开不冻结停开前已经登记的修读，也不取消已有学分；本次交付只保障
// 现有行为，不增加成绩状态或成绩更正功能。每条命令都重新打开记录文件，
// 因此提交反馈、学生记录（show）与学分核对（check）之间的一致性同时
// 覆盖落盘后再加载的结果：
//   - 开放时登记、尚无结果的修读，在课程随后停开后仍可提交通过：退出码 0
//     并显示提交成功；学生记录只把这份修读改为通过（学生、要求、学期、
//     修读编号不变），同一要求下其他选课修读不被一并改成通过；核对把该
//     要求列为已满足、计入课程学分并以这份通过修读说明来源；课程仍显示
//     停开，停开不能让核对漏掉这次通过获得的学分；
//   - 停开后把已有修读提交为未通过同样成功：记录保留未通过，该要求没有
//     其他通过修读或有效免修时仍列为未满足、不得学分，提交成功不代表
//     修读通过；其他要求已获得的学分继续保留，总学分与逐项判定都要核对；
//   - 停开期间用新修读编号为同一要求登记修读仍被拒绝（退出码 1，错误
//     说明指出课程停开，无登记成功提示），历史中没有新修读、文件不变；
//     这次拒绝不影响随后为另一份停开前已登记、仍选课的旧修读提交成绩，
//     从而区分“新增修读被禁止”与“旧修读可以继续提交成绩”。

// TestCLIClosedCourseSubmitPassForExistingEnrollment 课程开放时学生已为一项
// 尚未满足的要求登记修读且未提交结果；课程随后停开，仍允许把这份已有修读
// 提交为通过，并保证提交反馈、学生记录与学分核对三者一致。
func TestCLIClosedCourseSubmitPassForExistingEnrollment(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"enroll", "s1", "r1", "2024秋", "e2"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	// 课程随后停开：此时 r1 尚没有通过修读或有效免修，e1/e2 都仍是选课。
	if _, errText, code := runCLI(t, file, "course-close", "c1"); code != 0 {
		t.Fatalf("停开课程失败 code=%d err=%q", code, errText)
	}

	// 停开前已登记的修读 e1 仍可提交通过：退出码 0，反馈明确提交成功与结果。
	out, errText, code := runCLI(t, file, "pass", "s1", "e1")
	if code != 0 {
		t.Fatalf("停开后应为已有修读提交通过，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(out, "修读 e1 结果已提交：通过") {
		t.Fatalf("应显示修读 e1 提交通过成功，out=%q", out)
	}

	// 提交成绩后课程仍显示停开。
	if out, _, code := runCLI(t, file, "list-courses"); code != 0 ||
		!strings.Contains(out, "课程 c1《高等数学》4 学分，状态：停开") {
		t.Fatalf("提交成绩后课程应仍为停开，code=%d out=%q", code, out)
	}

	// 学生记录：e1 变为通过，所属学生、要求、学期与修读编号保持原样；
	// 同一要求下另一份尚未提交结果的修读 e2 仍是选课，不能被一并改成通过。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "学生 s1") ||
		!strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：通过") {
		t.Fatalf("e1 应记录为通过且学生/要求/学期/编号不变，out=%q", out)
	}
	if !strings.Contains(out, "修读 e2：要求 r1，学期 2024秋，结果：选课") {
		t.Fatalf("e2 应保持选课，不能被一并改成通过，out=%q", out)
	}

	// 核对：r1 列为已满足、计入 c1 的 4 学分，以这份通过修读 e1 说明来源；
	// 课程停开不能让核对漏掉这次通过获得的学分；选课的 e2 不计为另一次通过。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：已满足，来源为通过修读 e1") ||
		strings.Contains(out, "未满足要求：[") ||
		strings.Contains(out, "共通过 2 次") {
		t.Fatalf("停开后 e1 通过应满足 r1、计 4 学分且仅以 e1 说明来源，out=%q", out)
	}
}

// TestCLIClosedCourseSubmitFailForExistingEnrollment 停开后把已有修读提交为
// 未通过同样成功，但未通过不满足要求、不得学分；其他要求已获得的学分继续
// 保留。总学分与逐项判定分别核对，避免只看总数遗漏当前要求的判定错误。
func TestCLIClosedCourseSubmitFailForExistingEnrollment(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"course", "c2", "大学物理", "3"},
		{"req", "s1", "r1", "c1"},
		{"req", "s1", "r2", "c2"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"enroll", "s1", "r2", "2024春", "e2"},
		{"pass", "s1", "e2"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	// c1 随后停开；r1 没有任何通过修读或有效免修，e1 仍是选课。
	if _, errText, code := runCLI(t, file, "course-close", "c1"); code != 0 {
		t.Fatalf("停开课程失败 code=%d err=%q", code, errText)
	}

	// 停开后把已有修读 e1 提交为未通过：操作成功（退出码 0），反馈说明是未通过，
	// 不能出现“通过”字样的提交反馈。
	out, errText, code := runCLI(t, file, "fail", "s1", "e1")
	if code != 0 {
		t.Fatalf("停开后应为已有修读提交未通过，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(out, "修读 e1 结果已提交：未通过") {
		t.Fatalf("应显示修读 e1 提交未通过成功，out=%q", out)
	}

	// 学生记录保留未通过结果，学生/要求/学期/编号不变；e2 的通过保持原样。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：未通过") {
		t.Fatalf("e1 应保留未通过结果且归属信息不变，out=%q", out)
	}
	if !strings.Contains(out, "修读 e2：要求 r2，学期 2024春，结果：通过") {
		t.Fatalf("另一要求上 e2 的通过结果应继续保留，out=%q", out)
	}

	// 核对不能把“成绩提交成功”当成“修读通过”：r1 仍逐项列为未满足、不得学分；
	// 其他要求 r2 已获得的 3 学分继续保留。总学分与逐项判定都要核对。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：3") {
		t.Fatalf("只有 r2 的 3 学分应计入，未通过的 r1 不得学分，out=%q", out)
	}
	if !strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：未满足") ||
		!strings.Contains(out, "未满足要求：[r1]") ||
		strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("r1 未通过后应仍逐项列为未满足且没有通过来源，out=%q", out)
	}
	if !strings.Contains(out,
		"要求 r2（课程 c2《大学物理》，3 学分）：已满足，来源为通过修读 e2") {
		t.Fatalf("r2 已获得的学分与来源应继续保留，out=%q", out)
	}

	// 提交未通过后课程仍停开。
	if out, _, _ := runCLI(t, file, "list-courses"); !strings.Contains(out,
		"课程 c1《高等数学》4 学分，状态：停开") {
		t.Fatalf("提交未通过后 c1 应仍为停开，out=%q", out)
	}
}

// TestCLIClosedCourseBlocksNewEnrollmentButNotOldResultSubmit 停开期间用新的
// 修读编号登记修读仍按现有规则拒绝；这次拒绝不能影响随后为另一份停开前已
// 登记、仍处于选课状态的旧修读提交成绩，回归保障须能区分这两条路径。
func TestCLIClosedCourseBlocksNewEnrollmentButNotOldResultSubmit(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"course-close", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 停开期间改用新修读编号 e2 为同一要求登记修读：退出码 1，错误说明指出
	// 课程停开，不能出现登记成功的提示。
	before := mustReadRecord(t, file)
	out, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2025春", "e2")
	if code != 1 {
		t.Fatalf("停开后新增修读应拒绝且退出码 1，code=%d out=%q err=%q", code, out, errText)
	}
	if out != "" || strings.Contains(out, "已登记修读") || strings.Contains(out, "成功") {
		t.Fatalf("被拒绝时不应出现登记成功提示，out=%q", out)
	}
	if !strings.Contains(errText, "课程 c1 已停开，不能新增修读") {
		t.Fatalf("错误说明应指出课程 c1 停开，err=%q", errText)
	}

	// 学生历史中没有这份新修读；已保存的修读 e1 保持选课，记录文件原样不变，
	// 核对仍为 0 学分、r1 未满足。
	assertRecordUnchanged(t, file, before, "停开后新增修读被拒绝：")
	show, _, _ := runCLI(t, file, "show", "s1")
	if strings.Contains(show, "修读 e2") ||
		!strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("历史中不应出现新修读 e2，e1 应保持选课，out=%q", show)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("拒绝新增后应仍为 0 学分、r1 未满足，code=%d out=%q", code, out)
	}

	// 这次拒绝不能影响随后为另一份停开前已登记、仍处于选课状态的修读提交成绩。
	out, errText, code = runCLI(t, file, "pass", "s1", "e1")
	if code != 0 || !strings.Contains(out, "修读 e1 结果已提交：通过") {
		t.Fatalf("拒绝新增后仍应为旧修读 e1 提交通过，code=%d out=%q err=%q",
			code, out, errText)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "已满足，来源为通过修读 e1") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("旧修读 e1 提交通过后应满足 r1、计 4 学分，code=%d out=%q", code, out)
	}
	show, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：通过") ||
		strings.Contains(show, "修读 e2") {
		t.Fatalf("e1 应变为通过，历史中仍不应有被拒绝的 e2，out=%q", show)
	}

	// 新增修读被禁止的规则在提交成绩后仍然有效，与“旧修读可以提交成绩”并存。
	_, errText, code = runCLI(t, file, "enroll", "s1", "r1", "2025春", "e3")
	if code != 1 || !strings.Contains(errText, "课程 c1 已停开，不能新增修读") {
		t.Fatalf("提交成绩后新增修读仍应被拒绝并指出停开，code=%d err=%q", code, errText)
	}
	if show, _, _ := runCLI(t, file, "show", "s1"); strings.Contains(show, "修读 e3") {
		t.Fatalf("再次拒绝后历史中不应出现 e3，out=%q", show)
	}
}
