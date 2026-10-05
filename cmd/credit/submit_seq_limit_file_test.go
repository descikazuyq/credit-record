package main

import (
	"os"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“成绩提交顺序编号用尽”这一边界。编号无法经正常
// 命令逐步累加到 9223372036854775807，因此用例直接写出结构完整、可解析、
// nextResultSeq 已到上限（或差一）的记录文件，每条断言都重新打开进程：
//   - 计数器已达上限时，对仍处于选课的修读执行 pass/fail 都以退出码 1
//     拒绝，标准错误点名学生与修读编号并说明顺序编号已用尽、无法提交新
//     成绩；标准输出不出现提交成功或返回已提交成绩的提示；修读保持选课、
//     不获得学分，记录文件逐字节保持原样；
//   - 计数距上限恰好还差一时，pass/fail 都能完成最后一次正常首次提交并
//     落盘，提交先后关系保留；随后任何新的首次提交都被拒绝；
//   - 计数器到顶后，重复提交相同结果仍幂等成功返回原记录、不改文件；
//     改成另一结果仍按原有冲突规则拒绝；
//   - 即使该要求已有另一份通过修读或已由有效免修满足，也不放行新成绩；
//   - 学生或本人名下修读不存在时仍给原有的不存在提示；
//   - 编号用尽不是文件损坏：check/show/list-courses 正常工作且只读不改。

// seqLimitBase 构造结构完整、引用齐全的基础记录：s1 的要求 r1 指向
// 4 学分课程 c1。计数器与修读由各用例自行补充。
func seqLimitBase() *diskRecord {
	return &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
	}
}

const maxIntText = "9223372036854775807"

// assertSeqExhaustedCLI 断言一次首次成绩提交在编号用尽时被拒绝：退出码 1、
// stdout 无任何提交提示，stderr 点名学生、修读并说明编号已用尽。
func assertSeqExhaustedCLI(t *testing.T, file string, args []string, student, enr, label string) {
	t.Helper()
	out, errText, code := runCLI(t, file, args...)
	if code != exitRejected {
		t.Fatalf("%s：编号用尽应按业务规则退出码 1，code=%d out=%q err=%q",
			label, code, out, errText)
	}
	if out != "" || strings.Contains(out, "结果已提交") ||
		strings.Contains(out, "已提交过相同结果") {
		t.Fatalf("%s：被拒绝时标准输出不能出现提交成功或返回已提交成绩的提示，out=%q",
			label, out)
	}
	for _, want := range []string{
		student, enr, "成绩提交顺序编号已用尽", "无法提交新的成绩", maxIntText,
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s：标准错误应包含 %q，err=%q", label, want, errText)
		}
	}
	if strings.Contains(errText, "损坏") {
		t.Fatalf("%s：编号用尽不是记录文件损坏，err=%q", label, errText)
	}
}

// TestCLISubmitResultSeqExhaustedRejectsPassAndFail 计数器到顶后，对仍处于
// 选课的修读 pass/fail 都必须退出码 1：不输出成功提示、修读保持选课、
// 没有学分，记录文件逐字节不变，核对结论不变。
func TestCLISubmitResultSeqExhaustedRejectsPassAndFail(t *testing.T) {
	d := seqLimitBase()
	d.Enrollments = []diskEnr{
		// 一份早先的通过占掉 MaxInt-1，另有仍处于选课的 e2。
		enrRecord("e1", "s1", "r1", "2024春", "passed", 9223372036854775806),
		{ID: "e2", Student: "s1", Req: "r1", Term: "2024秋", Result: "enrolled"},
	}
	d.NextResultSeq = 9223372036854775807
	file, raw := writeDiskRecord(t, d)

	for _, cmd := range []string{"pass", "fail"} {
		assertSeqExhaustedCLI(t, file, []string{cmd, "s1", "e2"}, "s1", "e2", cmd+" 选课修读")
		show, _, code := runCLI(t, file, "show", "s1")
		if code != 0 || !strings.Contains(show, "修读 e2：要求 r1，学期 2024秋，结果：选课") {
			t.Fatalf("%s 被拒后 e2 应保持选课，code=%d show=%q", cmd, code, show)
		}
		// 原有的通过与来源判定不变。
		out, _, code := runCLI(t, file, "check", "s1")
		if code != 0 || !strings.Contains(out, "总学分：4") ||
			!strings.Contains(out, "来源为通过修读 e1") {
			t.Fatalf("%s 被拒后核对结论应不变，code=%d out=%q", cmd, code, out)
		}
		assertFileByteIdentical(t, file, raw, cmd+" 拒绝之后：")
	}
}

// TestCLISubmitResultSeqLastSlotSubmission 计数距上限差一时，最后一次首次
// 提交正常完成并落盘（pass 与 fail 两种入口都覆盖），提交先后关系保留；
// 到顶之后新的首次提交被拒绝。
func TestCLISubmitResultSeqLastSlotSubmission(t *testing.T) {
	t.Run("最后一次提交通过", func(t *testing.T) {
		d := seqLimitBase()
		d.Enrollments = []diskEnr{
			enrRecord("e1", "s1", "r1", "2024春", "failed", 9223372036854775806),
			{ID: "e2", Student: "s1", Req: "r1", Term: "2024秋", Result: "enrolled"},
			{ID: "e3", Student: "s1", Req: "r1", Term: "2025春", Result: "enrolled"},
		}
		d.NextResultSeq = 9223372036854775806
		file, _ := writeDiskRecord(t, d)

		// 距上限恰好还差一：最后一次合法的首次提交。
		out, _, code := runCLI(t, file, "pass", "s1", "e2")
		if code != 0 || !strings.Contains(out, "修读 e2 结果已提交：通过") {
			t.Fatalf("差一号时应允许最后一次通过提交，code=%d out=%q", code, out)
		}
		// 落盘后再打开：e2 取得 MaxInt 序号；check/show 正常，来源是 e2
		// 而不是更早的未通过 e1。
		check, _, code := runCLI(t, file, "check", "s1")
		if code != 0 || !strings.Contains(check, "总学分：4") ||
			!strings.Contains(check, "来源为通过修读 e2") {
			t.Fatalf("最后一次通过后应以 e2 说明来源，code=%d check=%q", code, check)
		}
		show, _, _ := runCLI(t, file, "show", "s1")
		if !strings.Contains(show, "修读 e2：要求 r1，学期 2024秋，结果：通过") {
			t.Fatalf("e2 应显示为通过，show=%q", show)
		}

		// 到顶后 e3 的首次提交被拒绝，e2 的结果与序号保留。
		assertSeqExhaustedCLI(t, file, []string{"pass", "s1", "e3"}, "s1", "e3", "到顶后 pass")
		show, _, _ = runCLI(t, file, "show", "s1")
		if !strings.Contains(show, "修读 e3：要求 r1，学期 2025春，结果：选课") ||
			!strings.Contains(show, "结果：通过") {
			t.Fatalf("e3 应保持选课、e2 保持通过，show=%q", show)
		}
	})

	t.Run("最后一次提交未通过", func(t *testing.T) {
		d := seqLimitBase()
		d.Enrollments = []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
			{ID: "e2", Student: "s1", Req: "r1", Term: "2024秋", Result: "enrolled"},
		}
		d.NextResultSeq = 9223372036854775806
		file, _ := writeDiskRecord(t, d)

		// 未通过同样占用最后一次提交机会。
		out, _, code := runCLI(t, file, "fail", "s1", "e1")
		if code != 0 || !strings.Contains(out, "修读 e1 结果已提交：未通过") {
			t.Fatalf("差一号时未通过也应成功，code=%d out=%q", code, out)
		}
		check, _, code := runCLI(t, file, "check", "s1")
		if code != 0 || !strings.Contains(check, "总学分：0") ||
			!strings.Contains(check, "未满足要求：[r1]") {
			t.Fatalf("最后一次未通过仍不计学分，code=%d check=%q", code, check)
		}
		// 此后通过也无法再首次提交。
		afterSave, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		assertSeqExhaustedCLI(t, file, []string{"pass", "s1", "e2"}, "s1", "e2", "到顶后 pass")
		assertFileByteIdentical(t, file, afterSave, "到顶拒绝之后：")
		// e1 保持未通过、e2 保持选课。
		show, _, _ := runCLI(t, file, "show", "s1")
		if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：未通过") ||
			!strings.Contains(show, "修读 e2：要求 r1，学期 2024秋，结果：选课") {
			t.Fatalf("两条修读结果应保持，show=%q", show)
		}
	})
}

// TestCLISubmitResultSeqExhaustedIdempotentAndConflict 到顶后：重复提交相同
// 结果幂等成功并返回原记录、不改文件；改成另一结果按原有冲突规则拒绝，
// 错误不能说成编号用尽。
func TestCLISubmitResultSeqExhaustedIdempotentAndConflict(t *testing.T) {
	d := seqLimitBase()
	d.Courses = append(d.Courses, diskCourse{ID: "c2", Name: "大学物理", Credit: 3, Open: true})
	d.Requirements = append(d.Requirements, diskReq{ID: "r2", Student: "s1", Course: "c2"})
	d.Enrollments = []diskEnr{
		enrRecord("e1", "s1", "r1", "2024春", "passed", 9223372036854775806),
		enrRecord("e2", "s1", "r2", "2024春", "failed", 9223372036854775807),
	}
	d.NextResultSeq = 9223372036854775807
	file, raw := writeDiskRecord(t, d)

	// 重复提交相同结果：幂等成功（退出码 0），返回原记录、不增学分。
	out, _, code := runCLI(t, file, "pass", "s1", "e1")
	if code != 0 || !strings.Contains(out, "已提交过相同结果") ||
		!strings.Contains(out, "通过") {
		t.Fatalf("到顶后重复通过应幂等返回原记录，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "fail", "s1", "e2")
	if code != 0 || !strings.Contains(out, "已提交过相同结果") ||
		!strings.Contains(out, "未通过") {
		t.Fatalf("到顶后重复未通过应幂等返回原记录，code=%d out=%q", code, out)
	}
	assertFileByteIdentical(t, file, raw, "幂等重复之后：")
	// 学分仍是 r1 的一份 4（r2 未通过不计）。
	check, _, _ := runCLI(t, file, "check", "s1")
	if !strings.Contains(check, "总学分：4") {
		t.Fatalf("幂等重复不应改变学分，check=%q", check)
	}

	// 改成另一结果：原有冲突规则，退出码 1，不能报编号用尽。
	out, errText, code := runCLI(t, file, "fail", "s1", "e1")
	if code != exitRejected || out != "" || !strings.Contains(errText, "不能改为") {
		t.Fatalf("通过改未通过应按冲突拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	if strings.Contains(errText, "编号已用尽") {
		t.Fatalf("改结果冲突不能报成编号用尽，err=%q", errText)
	}
	out, errText, code = runCLI(t, file, "pass", "s1", "e2")
	if code != exitRejected || out != "" || !strings.Contains(errText, "不能改为") {
		t.Fatalf("未通过改通过应按冲突拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	assertFileByteIdentical(t, file, raw, "冲突拒绝之后：")
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：通过") ||
		!strings.Contains(show, "修读 e2：要求 r2，学期 2024春，结果：未通过") {
		t.Fatalf("原结果都应保留，show=%q", show)
	}
}

// TestCLISubmitResultSeqExhaustedNotBypassedByPassOrWaiver 要求已由另一份
// 通过修读或有效免修满足时，编号到顶后新的首次成绩提交仍被拒绝。
func TestCLISubmitResultSeqExhaustedNotBypassedByPassOrWaiver(t *testing.T) {
	t.Run("同一要求已有另一份通过", func(t *testing.T) {
		d := seqLimitBase()
		d.Enrollments = []diskEnr{
			enrRecord("e1", "s1", "r1", "2024春", "passed", 9223372036854775807),
			{ID: "e2", Student: "s1", Req: "r1", Term: "2024秋", Result: "enrolled"},
		}
		d.NextResultSeq = 9223372036854775807
		file, raw := writeDiskRecord(t, d)

		assertSeqExhaustedCLI(t, file, []string{"pass", "s1", "e2"}, "s1", "e2", "已有通过时 pass")
		check, _, _ := runCLI(t, file, "check", "s1")
		if !strings.Contains(check, "来源为通过修读 e1") ||
			strings.Contains(check, "共通过 2 次") {
			t.Fatalf("被拒后来源应仍是 e1、通过历史只有一条，check=%q", check)
		}
		assertFileByteIdentical(t, file, raw, "已有通过拒绝之后：")
	})

	t.Run("要求已由有效免修满足", func(t *testing.T) {
		d := seqLimitBase()
		d.Waivers = []diskWaiver{{
			ID: "w1", Student: "s1", Req: "r1", Basis: "学科竞赛获奖", Status: "approved",
		}}
		d.Enrollments = []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
		}
		d.NextResultSeq = 9223372036854775807
		file, raw := writeDiskRecord(t, d)

		// 免修已满足要求，但新成绩仍不能提交。
		assertSeqExhaustedCLI(t, file, []string{"pass", "s1", "e1"}, "s1", "e1", "有效免修时 pass")
		assertSeqExhaustedCLI(t, file, []string{"fail", "s1", "e1"}, "s1", "e1", "有效免修时 fail")
		check, _, code := runCLI(t, file, "check", "s1")
		if code != 0 || !strings.Contains(check, "总学分：4") ||
			!strings.Contains(check, "来源为有效免修 w1") {
			t.Fatalf("有效免修的满足状态与来源应不变，code=%d check=%q", code, check)
		}
		show, _, _ := runCLI(t, file, "show", "s1")
		if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：选课") {
			t.Fatalf("e1 应保持选课，show=%q", show)
		}
		assertFileByteIdentical(t, file, raw, "免修情形拒绝之后：")
	})
}

// TestCLISubmitResultSeqExhaustedUnknownStudentAndEnrollment 编号到顶后，
// 学生或本人名下修读不存在仍给原有的不存在提示（退出码 1），不能借用
// 他人修读，也不改文件。
func TestCLISubmitResultSeqExhaustedUnknownStudentAndEnrollment(t *testing.T) {
	d := seqLimitBase()
	d.Students = append(d.Students, diskStudent{ID: "s2"})
	d.Enrollments = []diskEnr{
		enrRecord("e1", "s1", "r1", "2024春", "passed", 9223372036854775807),
	}
	d.NextResultSeq = 9223372036854775807
	file, raw := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "pass", "ghost", "e1")
	if code != exitRejected || out != "" || !strings.Contains(errText, "学生 ghost 不存在") {
		t.Fatalf("不存在学生应走原有报错，code=%d out=%q err=%q", code, out, errText)
	}
	out, errText, code = runCLI(t, file, "pass", "s1", "nope")
	if code != exitRejected || out != "" ||
		!strings.Contains(errText, "学生 s1 名下不存在修读 nope") {
		t.Fatalf("不存在修读应走原有报错，code=%d out=%q err=%q", code, out, errText)
	}
	out, errText, code = runCLI(t, file, "fail", "s2", "e1")
	if code != exitRejected || out != "" ||
		!strings.Contains(errText, "学生 s2 名下不存在修读 e1") {
		t.Fatalf("借名提交应按本人名下不存在报错，code=%d out=%q err=%q", code, out, errText)
	}
	assertFileByteIdentical(t, file, raw, "不存在类报错之后：")
}

// TestCLISubmitResultSeqExhaustedFileStaysReadable 编号到顶的合法记录不是
// 内容损坏：check/show/list-courses 都正常，只读访问逐字节不改文件；
// 编号到顶后整个文件无法再写入新的首次成绩，但已有学分与来源照常可查。
func TestCLISubmitResultSeqExhaustedFileStaysReadable(t *testing.T) {
	d := seqLimitBase()
	d.Enrollments = []diskEnr{
		enrRecord("e1", "s1", "r1", "2024春", "passed", 9223372036854775807),
		{ID: "e2", Student: "s1", Req: "r1", Term: "2024秋", Result: "enrolled"},
	}
	d.NextResultSeq = 9223372036854775807
	file, raw := writeDiskRecord(t, d)

	// 只读命令全部正常，且不改文件。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") ||
		!strings.Contains(out, "未满足要求：（无）") {
		t.Fatalf("到顶记录应正常核对，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 || !strings.Contains(out, "结果：通过") || !strings.Contains(out, "结果：选课") {
		t.Fatalf("到顶记录应可正常查看，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "list-courses")
	if code != 0 || !strings.Contains(out, "课程 c1《高等数学》4 学分") {
		t.Fatalf("到顶记录应可正常列出课程，code=%d out=%q", code, out)
	}
	assertFileByteIdentical(t, file, raw, "只读访问之后：")

	// 不存在学生仍走原有不存在提示，而不是文件损坏。
	_, errText, code := runCLI(t, file, "check", "ghost")
	if code != exitRejected || !strings.Contains(errText, "不存在") {
		t.Fatalf("不存在学生行为应不变，code=%d err=%q", code, errText)
	}
	assertFileByteIdentical(t, file, raw, "不存在查询之后：")

	// 新成绩被业务规则拒绝（退出码 1），而不是按损坏文件拒绝（退出码 2）。
	assertSeqExhaustedCLI(t, file, []string{"pass", "s1", "e2"}, "s1", "e2", "到顶记录 pass")
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("拒绝提交不得改写记录文件\nwant=%q\n got=%q", raw, got)
	}
}
