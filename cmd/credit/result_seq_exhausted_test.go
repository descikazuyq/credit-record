package main

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“成绩提交顺序编号用尽时的首次成绩提交”。记录文件
// 中的成绩提交计数（nextResultSeq）达到程序整数类型 int 的最大值
// （64 位环境下为 9223372036854775807）后，若再给仍处于“选课”的修读首次
// 提交成绩，递增会回绕成负数并落盘，整份文件下次读取即被判内容损坏，连
// 原有记录也无法读出。这类计数无法经正常提交流程达到，所以用例直接写出
// 结构完整、可解析、计数已到上限的记录文件，每条断言都重新打开进程访问：
//   - 计数到顶时 pass/fail 首次提交都以退出码 1 拒绝：stderr 点名学生与
//     修读编号并说明顺序编号已用尽，stdout 不出现提交成功提示；原修读保持
//     选课、不获得学分，整份记录文件逐字节不变；
//   - 该要求另有通过修读或已由有效免修满足，也不放行这次新成绩；
//   - 计数距上限恰好还差一时允许一次正常的首次提交（序号恰为最大值），
//     通过计学分、未通过不计；此后计数到顶，下一份选课修读被拒绝；
//   - 已提交过结果的修读不需要新序号：重复提交相同结果仍幂等成功且不改
//     文件，改提另一结果仍按原有冲突规则拒绝；
//   - 学生或修读不存在时仍报原有的不存在提示；
//   - 编号用尽本身不是内容损坏：check/show 照常，已有学分、免修状态与
//     来源判定保持原有规则。

// seqExhaustedBase 构造计数已到上限的基础记录：s1 的要求 r1 指向 4 学分
// 课程 c1，修读列表由各用例拼入。
func seqExhaustedBase() *diskRecord {
	d := seqDupBase()
	d.NextResultSeq = math.MaxInt
	return d
}

// assertSeqExhaustedRejected 断言一次首次成绩提交被“编号用尽”拒绝：
// 退出码 1，stdout 没有任何提交成功提示，stderr 点名学生、修读编号并
// 说明顺序编号已用尽。
func assertSeqExhaustedRejected(t *testing.T, file string, args []string, student, enr, label string) {
	t.Helper()
	out, errText, code := runCLI(t, file, args...)
	if code != exitRejected {
		t.Fatalf("%s：编号用尽时首次提交应退出码 %d，code=%d out=%q err=%q",
			label, exitRejected, code, out, errText)
	}
	if out != "" {
		t.Fatalf("%s：被拒绝的提交不得在标准输出报告成功，out=%q", label, out)
	}
	for _, want := range []string{student, enr, "用尽", strconv.Itoa(math.MaxInt)} {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s：错误应点名学生、修读并说明编号用尽（缺 %q），err=%q",
				label, want, errText)
		}
	}
	if strings.Contains(errText, "损坏") {
		t.Fatalf("%s：编号用尽不是记录文件损坏，err=%q", label, errText)
	}
}

// TestCLISeqExhaustedRejectsFirstSubmission 计数到顶时，对选课中的修读首次
// 提交通过或未通过都被拒绝；原修读保持选课、不获得学分，文件逐字节不变，
// check/show 仍正常（编号用尽不是内容损坏）。
func TestCLISeqExhaustedRejectsFirstSubmission(t *testing.T) {
	for _, cmd := range []string{"pass", "fail"} {
		d := seqExhaustedBase()
		d.Enrollments = []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
		}
		file, raw := writeDiskRecord(t, d)

		assertSeqExhaustedRejected(t, file, []string{cmd, "s1", "e1"}, "s1", "e1", cmd+" 首次提交")
		assertFileByteIdentical(t, file, raw, cmd+" 拒绝后：")

		// 原修读仍保持选课。
		show, _, code := runCLI(t, file, "show", "s1")
		if code != 0 || !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：选课") {
			t.Fatalf("%s 拒绝后修读应保持选课，code=%d out=%q", cmd, code, show)
		}
		// 核对正常：未获得学分，要求未满足。
		out, _, code := runCLI(t, file, "check", "s1")
		if code != 0 || !strings.Contains(out, "总学分：0") ||
			!strings.Contains(out, "未满足要求：[r1]") {
			t.Fatalf("%s 拒绝后核对应正常且不计学分，code=%d out=%q", cmd, code, out)
		}
		assertFileByteIdentical(t, file, raw, cmd+" 只读访问后：")
	}
}

// TestCLISeqExhaustedNotSatisfiedByOtherPassOrWaiver 即使该要求已有另一份
// 通过修读、或已由有效免修满足，计数到顶时仍不能为选课中的修读首次提交
// 成绩；已有学分、免修状态与来源判定保持原样。
func TestCLISeqExhaustedNotSatisfiedByOtherPassOrWaiver(t *testing.T) {
	// 该要求另有通过修读 e0（序号 1）。
	d := seqExhaustedBase()
	d.Enrollments = []diskEnr{
		enrRecord("e0", "s1", "r1", "2024春", "passed", 1),
		{ID: "e1", Student: "s1", Req: "r1", Term: "2024秋", Result: "enrolled"},
	}
	file, raw := writeDiskRecord(t, d)
	assertSeqExhaustedRejected(t, file, []string{"pass", "s1", "e1"}, "s1", "e1", "另有通过修读")
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e0") {
		t.Fatalf("已有通过与来源判定应保持不变，code=%d out=%q", code, out)
	}
	assertFileByteIdentical(t, file, raw, "另有通过修读拒绝后：")

	// 该要求已由有效免修满足。
	d = seqExhaustedBase()
	d.Enrollments = []diskEnr{
		{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
	}
	d.Waivers = []diskWaiver{
		{ID: "w1", Student: "s1", Req: "r1", Basis: "竞赛获奖", Status: "approved"},
	}
	file, raw = writeDiskRecord(t, d)
	assertSeqExhaustedRejected(t, file, []string{"fail", "s1", "e1"}, "s1", "e1", "已有有效免修")
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("免修状态与学分应保持不变，code=%d out=%q", code, out)
	}
	assertFileByteIdentical(t, file, raw, "已有有效免修拒绝后：")
}

// TestCLISeqOneBelowMaxAllowsLastSubmission 计数距上限恰好还差一时，允许
// 一次正常的首次提交：序号恰为最大值、通过计学分；随后计数到顶，下一份
// 选课修读的首次提交被拒绝，最后一次合法提交的先后关系保留。
func TestCLISeqOneBelowMaxAllowsLastSubmission(t *testing.T) {
	d := seqDupBase()
	d.NextResultSeq = math.MaxInt - 1
	d.Enrollments = []diskEnr{
		{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
		{ID: "e2", Student: "s1", Req: "r1", Term: "2024秋", Result: "enrolled"},
	}
	file, _ := writeDiskRecord(t, d)

	// 最后一次合法提交：通过，占用序号上限。
	out, errText, code := runCLI(t, file, "pass", "s1", "e1")
	if code != 0 || !strings.Contains(out, "修读 e1 结果已提交：通过") {
		t.Fatalf("距上限差一时应允许最后一次首次提交，code=%d out=%q err=%q", code, out, errText)
	}
	// 计数到顶：下一份选课修读首次提交被拒绝，文件不再变化。
	before := mustReadRecord(t, file)
	assertSeqExhaustedRejected(t, file, []string{"fail", "s1", "e2"}, "s1", "e2", "计数到顶后的首次提交")
	assertRecordUnchanged(t, file, before, "计数到顶后的拒绝：")

	// 最后一次合法提交的先后关系保留：e1 以最大序号成为来源，计 4 学分。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("最后一次通过应保留为来源并计学分，code=%d out=%q", code, out)
	}
}

// TestCLISeqExhaustedIdempotentAndConflictUnaffected 已提交过结果的修读不
// 需要新序号：计数到顶时重复提交相同结果仍成功返回原记录、不增加学分、
// 不改文件；改提另一结果仍按原有冲突规则拒绝。
func TestCLISeqExhaustedIdempotentAndConflictUnaffected(t *testing.T) {
	d := seqExhaustedBase()
	d.Enrollments = []diskEnr{
		enrRecord("e1", "s1", "r1", "2024春", "passed", math.MaxInt),
	}
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "pass", "s1", "e1")
	if code != 0 || !strings.Contains(out, "返回原记录") {
		t.Fatalf("重复提交相同结果应幂等成功，code=%d out=%q", code, out)
	}
	assertFileByteIdentical(t, file, raw, "幂等重复后：")

	out, errText, code := runCLI(t, file, "fail", "s1", "e1")
	if code != exitRejected || !strings.Contains(errText, "不能改为") {
		t.Fatalf("改提另一结果仍应按冲突规则拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	assertFileByteIdentical(t, file, raw, "冲突拒绝后：")

	// 学分仍只计一份。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") {
		t.Fatalf("幂等重复不得增加学分，code=%d out=%q", code, out)
	}
}

// TestCLISeqExhaustedNotFoundUnaffected 计数到顶时，学生或修读不存在仍
// 报原有的不存在提示，与编号用尽无关。
func TestCLISeqExhaustedNotFoundUnaffected(t *testing.T) {
	d := seqExhaustedBase()
	d.Enrollments = []diskEnr{
		{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
	}
	file, raw := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "pass", "ghost", "e1")
	if code != exitRejected || out != "" || !strings.Contains(errText, "学生 ghost 不存在") {
		t.Fatalf("学生不存在应报原有提示，code=%d out=%q err=%q", code, out, errText)
	}
	out, errText, code = runCLI(t, file, "pass", "s1", "ghost")
	if code != exitRejected || out != "" || !strings.Contains(errText, "名下不存在修读 ghost") {
		t.Fatalf("修读不存在应报原有提示，code=%d out=%q err=%q", code, out, errText)
	}
	assertFileByteIdentical(t, file, raw, "不存在提示后：")
}
