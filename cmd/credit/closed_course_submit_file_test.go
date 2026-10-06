package main

import (
	"os"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“课程停开后，停开前已经登记的修读仍可提交成绩”
// 这一现有行为。停开只限制新增修读，不冻结停开前的修读、不取消已有学分；
// 本交付只保障现状，不引入成绩状态或成绩更正功能。每条断言都重新打开进程
// 访问同一个记录文件，因此同时覆盖落盘与重新加载后的一致性：
//   - 课程开放时登记、仍处于选课的修读，在课程停开后提交通过：退出码 0 且
//     显示提交成功；show 中结果变为通过，所属学生、要求、学期与修读编号
//     保持原样；check 将该要求列为已满足、计入对应课程学分并以这份通过
//     修读说明来源；课程仍显示停开；同一要求下其他尚未提交结果的修读仍是
//     选课，不能被一并改成通过；
//   - 停开后把已有修读提交为未通过同样成功（退出码 0），记录保留未通过；
//     该要求没有其他通过修读或有效免修时仍列为未满足、不获得学分，提交
//     成功不等于修读通过；学生在其他要求上已获得的学分继续保留，核对的
//     总学分与逐项判定都要与实际结果一致；
//   - 停开期间改用新修读编号登记仍按现有规则拒绝（退出码 1，错误说明课程
//     停开，无登记成功提示），学生历史没有这份新修读，记录文件逐字节不变；
//     这次拒绝不能影响随后为另一份停开前已登记、仍处于选课的修读提交成绩。

// setupCLIClosedCourseCourses 登记 s1、两门课程（c1 高等数学 4 学分、
// c2 大学物理 3 学分）以及学生分别指向两门课程的要求 r1、r2。
func setupCLIClosedCourseCourses(t *testing.T, file string) {
	t.Helper()
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"course", "c2", "大学物理", "3"},
		{"req", "s1", "r1", "c1"},
		{"req", "s1", "r2", "c2"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
}

// TestCLIClosedCourseSubmitPassForEarlierEnrollment 课程开放时登记的选课修读，
// 在课程停开后提交通过：命令成功并显示提交成功；学生记录中该修读变为通过、
// 学生/要求/学期/修读编号原样；核对把要求列为已满足、计入课程学分并以这份
// 通过修读说明来源；课程仍停开；同要求下另一条选课修读不能被一并改成通过。
func TestCLIClosedCourseSubmitPassForEarlierEnrollment(t *testing.T) {
	file := tempRecordFile(t)
	setupCLIClosedCourseCourses(t, file)

	// 课程开放时：r1 下登记两份修读 e1、e2（同学期、不同编号），r2 的 e9
	// 在停开前通过，用于核对其他要求已获学分在停开后继续保留。
	for _, st := range [][]string{
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"enroll", "s1", "r1", "2024春", "e2"},
		{"enroll", "s1", "r2", "2023秋", "e9"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("开放时步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	if _, _, code := runCLI(t, file, "pass", "s1", "e9"); code != 0 {
		t.Fatal("停开前提交 e9 通过应成功")
	}

	// 随后停开 c1：只限制新增修读。
	if out, _, code := runCLI(t, file, "course-close", "c1"); code != 0 ||
		!strings.Contains(out, "课程 c1《高等数学》状态：停开") {
		t.Fatalf("停开 c1 失败，code=%d out=%q", code, out)
	}

	// 停开后把停开前已登记、仍处于选课的 e1 提交为通过：退出码 0，显示成功。
	out, errText, code := runCLI(t, file, "pass", "s1", "e1")
	if code != exitOK {
		t.Fatalf("停开后为旧修读提交通过应成功且退出码 0，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(out, "修读 e1 结果已提交：通过") {
		t.Fatalf("应显示 e1 提交成功且结果为通过，out=%q", out)
	}

	// 学生记录：e1 变为通过，所属学生、要求、学期与修读编号保持原样；
	// 同一要求下尚未提交结果的 e2 仍是选课，不能被一并改成通过。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, show)
	}
	if !strings.Contains(show, "学生 s1") ||
		!strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：通过") {
		t.Fatalf("e1 应在 s1 名下、仍属 r1/2024春 且结果变为通过，show=%q", show)
	}
	if !strings.Contains(show, "修读 e2：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("同要求下尚未提交结果的 e2 应保持选课，show=%q", show)
	}
	if !strings.Contains(show, "修读 e9：要求 r2，学期 2023秋，结果：通过") {
		t.Fatalf("其他要求上已有的通过结果应原样保留，show=%q", show)
	}

	// 核对：r1 列为已满足、计入 c1 的 4 学分并以 e1 说明来源；r2 的 3 学分
	// （来源 e9）继续保留；即使课程已停开也不能漏掉 e1 这次通过的学分。
	check, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("停开后核对应成功，code=%d out=%q", code, check)
	}
	if !strings.Contains(check, "总学分：7") {
		t.Fatalf("总学分应为两门课程之和 7（4+3），check=%q", check)
	}
	if !strings.Contains(check, "要求 r1（课程 c1《高等数学》，4 学分）：已满足") ||
		!strings.Contains(check, "来源为通过修读 e1") {
		t.Fatalf("r1 应列为已满足并以 e1 说明来源，check=%q", check)
	}
	if !strings.Contains(check, "要求 r2（课程 c2《大学物理》，3 学分）：已满足") ||
		!strings.Contains(check, "来源为通过修读 e9") {
		t.Fatalf("r2 已获学分与来源应继续保留，check=%q", check)
	}
	if strings.Contains(check, "未满足要求：[") {
		t.Fatalf("两项要求都已满足，不应列出未满足要求，check=%q", check)
	}
	// e2 仍是选课：r1 下只有一次通过，不能出现“共通过 2 次”之类表述。
	if strings.Contains(check, "共通过 2 次") {
		t.Fatalf("e2 不能被一并算作通过，check=%q", check)
	}

	// 提交成绩不改变课程状态：c1 仍显示停开（c2 保持开放）。
	list, _, code := runCLI(t, file, "list-courses")
	if code != 0 {
		t.Fatalf("列出课程失败 code=%d out=%q", code, list)
	}
	if !strings.Contains(list, "课程 c1《高等数学》4 学分，状态：停开") {
		t.Fatalf("提交成绩后 c1 应仍停开，list=%q", list)
	}
	if !strings.Contains(list, "课程 c2《大学物理》3 学分，状态：开放") {
		t.Fatalf("c2 应保持开放，list=%q", list)
	}

	// 重新打开进程再核对一次：通过结果、学分、来源与停开状态都已持久化。
	check2, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(check2, "总学分：7") ||
		!strings.Contains(check2, "来源为通过修读 e1") {
		t.Fatalf("重新加载后 e1 的通过与 4 学分应仍在，code=%d check=%q", code, check2)
	}
}

// TestCLIClosedCourseSubmitFailForEarlierEnrollment 停开后把旧修读提交为
// 未通过同样成功：退出码 0、记录保留未通过；该要求没有其他通过修读或有效
// 免修时仍列为未满足、不获得学分，不能把提交成功当成修读通过；其他要求上
// 已获得的学分继续保留，总学分与逐项判定都要反映各项要求的实际结果。
func TestCLIClosedCourseSubmitFailForEarlierEnrollment(t *testing.T) {
	file := tempRecordFile(t)
	setupCLIClosedCourseCourses(t, file)

	// r1 下只有一份修读 e1（之后不安排其他通过修读或免修）；r2 的 e9 在
	// 停开前通过，保证学生在其他要求上已持有学分。
	for _, st := range [][]string{
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"enroll", "s1", "r2", "2023秋", "e9"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("开放时步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	if _, _, code := runCLI(t, file, "pass", "s1", "e9"); code != 0 {
		t.Fatal("停开前提交 e9 通过应成功")
	}
	if _, _, code := runCLI(t, file, "course-close", "c1"); code != 0 {
		t.Fatal("停开 c1 失败")
	}

	// 停开后提交未通过：操作成功、退出码 0，明确显示提交的是未通过。
	out, errText, code := runCLI(t, file, "fail", "s1", "e1")
	if code != exitOK {
		t.Fatalf("停开后为旧修读提交未通过应成功且退出码 0，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(out, "修读 e1 结果已提交：未通过") {
		t.Fatalf("应显示 e1 提交成功且结果为未通过，out=%q", out)
	}
	// 成功提示不能把未通过说成通过。
	if strings.Contains(out, "结果已提交：通过") {
		t.Fatalf("未通过的提交反馈不能出现通过，out=%q", out)
	}

	// 学生记录保留未通过结果，学生/要求/学期/修读编号原样。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 ||
		!strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：未通过") {
		t.Fatalf("e1 应保留未通过结果且归属信息不变，code=%d show=%q", code, show)
	}

	// 核对：r1 没有其他通过修读或有效免修，仍列为未满足、不得 4 学分；
	// 不能只看总学分——逐项判定也要明确 r1 未满足。r2 的 3 学分继续保留。
	check, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对应成功，code=%d out=%q", code, check)
	}
	if !strings.Contains(check, "总学分：3") {
		t.Fatalf("只有 r2 的 3 学分应计入（未通过不得 c1 的 4 学分），check=%q", check)
	}
	if !strings.Contains(check, "要求 r1（课程 c1《高等数学》，4 学分）：未满足") {
		t.Fatalf("r1 必须逐项列为未满足，不能只凭总学分掩盖，check=%q", check)
	}
	if !strings.Contains(check, "未满足要求：[r1]") {
		t.Fatalf("未满足要求应列出 r1，check=%q", check)
	}
	if strings.Contains(check, "来源为通过修读 e1") ||
		strings.Contains(check, "来源为有效免修") {
		t.Fatalf("未通过的 e1 不能成为学分来源，check=%q", check)
	}
	if !strings.Contains(check, "要求 r2（课程 c2《大学物理》，3 学分）：已满足") ||
		!strings.Contains(check, "来源为通过修读 e9") {
		t.Fatalf("其他要求上已获得的 3 学分与来源应继续保留，check=%q", check)
	}

	// 课程仍停开，未通过提交不改变课程状态。
	if list, _, code := runCLI(t, file, "list-courses"); code != 0 ||
		!strings.Contains(list, "课程 c1《高等数学》4 学分，状态：停开") {
		t.Fatalf("提交未通过后 c1 应仍停开，code=%d list=%q", code, list)
	}
}

// TestCLIClosedCourseNewEnrollmentRejectedDoesNotBlockOldSubmissions 停开期间
// 用新修读编号登记必须按现有规则拒绝：退出码 1、错误说明课程停开、无成功
// 提示，学生历史没有这份新修读，记录文件逐字节不变；该拒绝不能影响随后为
// 另一份停开前已登记、仍处于选课的修读提交成绩（通过、未通过两个入口都覆盖）。
func TestCLIClosedCourseNewEnrollmentRejectedDoesNotBlockOldSubmissions(t *testing.T) {
	file := tempRecordFile(t)
	setupCLIClosedCourseCourses(t, file)

	// 开放时登记两份之后要提交成绩的旧修读：e1 属 r1、e3 属 r2。
	for _, st := range [][]string{
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"enroll", "s1", "r2", "2024春", "e3"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("开放时步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	if _, _, code := runCLI(t, file, "course-close", "c1"); code != 0 {
		t.Fatal("停开 c1 失败")
	}
	if _, _, code := runCLI(t, file, "course-close", "c2"); code != 0 {
		t.Fatal("停开 c2 失败")
	}

	// 停开期间改用新编号 e2 为 r1 登记修读：退出码 1，错误点名课程停开。
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	out, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2025春", "e2")
	if code != exitRejected {
		t.Fatalf("停开后新增修读应按业务规则退出码 1，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(errText, "课程 c1 已停开，不能新增修读") {
		t.Fatalf("错误说明应指出课程 c1 停开，err=%q", errText)
	}
	if strings.Contains(out, "已登记修读") || strings.Contains(out, "成功") {
		t.Fatalf("被拒绝时标准输出不能出现登记成功提示，out=%q", out)
	}

	// 学生历史没有这份新修读；停开前的旧修读与成绩（都还处于选课）原样保留。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, show)
	}
	if strings.Contains(show, "修读 e2") {
		t.Fatalf("被拒绝的新修读 e2 不应进入学生历史，show=%q", show)
	}
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：选课") ||
		!strings.Contains(show, "修读 e3：要求 r2，学期 2024春，结果：选课") {
		t.Fatalf("两份停开前的旧修读应都保持选课，show=%q", show)
	}
	assertRecordUnchanged(t, file, string(before), "停开后新增修读被拒：")

	// 课程仍显示停开。
	if list, _, _ := runCLI(t, file, "list-courses"); !strings.Contains(list, "状态：停开") {
		t.Fatalf("被拒绝的登记不应改变停开状态，list=%q", list)
	}

	// 拒绝不能影响随后提交旧修读的成绩：e1 提交通过、e3 提交未通过都成功，
	// 保留 pass/fail 两个公开入口与现有业务结果。
	passOut, passErr, code := runCLI(t, file, "pass", "s1", "e1")
	if code != exitOK || !strings.Contains(passOut, "修读 e1 结果已提交：通过") {
		t.Fatalf("拒绝新增后仍应为旧修读 e1 提交通过，code=%d out=%q err=%q",
			code, passOut, passErr)
	}
	failOut, failErr, code := runCLI(t, file, "fail", "s1", "e3")
	if code != exitOK || !strings.Contains(failOut, "修读 e3 结果已提交：未通过") {
		t.Fatalf("拒绝新增后仍应为旧修读 e3 提交未通过，code=%d out=%q err=%q",
			code, failOut, failErr)
	}

	// 核对与两项实际结果一致：r1 凭 e1 满足、计 c1 的 4 学分；r2 未通过、
	// 未满足、不计学分；总学分 4，未满足要求只列 r2；新修读 e2 依旧不存在。
	check, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对应成功，code=%d out=%q", code, check)
	}
	if !strings.Contains(check, "总学分：4") {
		t.Fatalf("只应计入 e1 通过获得的 4 学分，check=%q", check)
	}
	if !strings.Contains(check, "要求 r1（课程 c1《高等数学》，4 学分）：已满足") ||
		!strings.Contains(check, "来源为通过修读 e1") {
		t.Fatalf("r1 应由 e1 的通过满足，check=%q", check)
	}
	if !strings.Contains(check, "要求 r2（课程 c2《大学物理》，3 学分）：未满足") ||
		!strings.Contains(check, "未满足要求：[r2]") {
		t.Fatalf("r2 的未通过应使要求保持未满足，check=%q", check)
	}

	show2, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, show2)
	}
	if strings.Contains(show2, "修读 e2") {
		t.Fatalf("后续提交成绩也不应补出被拒绝的 e2，show=%q", show2)
	}
	if !strings.Contains(show2, "修读 e1：要求 r1，学期 2024春，结果：通过") ||
		!strings.Contains(show2, "修读 e3：要求 r2，学期 2024春，结果：未通过") {
		t.Fatalf("两份旧修读应分别保留通过与未通过结果，show=%q", show2)
	}
}
