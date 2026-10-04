package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“读取已有记录时，有效免修的依据校验与首次正常
// 申请一致”。状态为有效但依据为空或全部为空白字符（空格、制表符、换行、
// 全角空格 U+3000、不换行空格 U+00A0 等，允许混用）的免修无法经正常申请
// 流程产生（首次提交会被业务拒绝），所以损坏用例直接写出结构完整的记录
// 文件，每条断言都重新打开进程访问它：
//   - check、show 乃至写入类命令都沿用文件内容损坏的退出码 2，标准输出
//     不出现总学分、要求已满足或操作成功的说明，错误输出点名记录文件
//     内容损坏、哪名学生的哪份有效免修缺少依据，原文件逐字节保留；
//   - 即使该要求另有通过修读也不能绕过校验继续核对；
//   - 依据含实际文字、只是前后/中间带缩进与换行时正常读取，show 中仍能
//     查到依据原文；
//   - 已拒绝申请依据为空白时记录仍合法，拒绝状态与原因照常可查、不计学分；
//   - 用户首次正常提交空白依据申请仍按业务规则拒绝（退出码 1），申请与
//     拒绝原因保留，随后重新打开文件是合法记录。

// blankBasisRecord 构造结构完整、引用齐全的记录：s1 的 r1 指向 4 学分
// 课程 c1。basis 决定有效免修 w1 的依据；withPass 时再附通过修读 e1。
func blankBasisRecord(basis string, withPass bool) *diskRecord {
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Waivers: []diskWaiver{
			{ID: "w1", Student: "s1", Req: "r1", Basis: basis, Status: "approved"},
		},
	}
	if withPass {
		d.Enrollments = []diskEnr{{
			ID: "e1", Student: "s1", Req: "r1", Term: "2024春",
			Result: "passed", ResultSeq: 1,
		}}
		d.NextResultSeq = 1
	}
	return d
}

// assertBlankBasisAccessRejected 用指定命令访问损坏文件：必须退出码 2、
// stdout 没有任何业务内容，stderr 点名文件损坏与学生、免修编号。
func assertBlankBasisAccessRejected(t *testing.T, file string, raw []byte,
	args []string, label string) {
	t.Helper()
	out, errText, code := runCLI(t, file, args...)
	if code != exitFile {
		t.Fatalf("%s：访问含空白依据有效免修的记录应退出码 %d，code=%d out=%q err=%q",
			label, exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("%s：拒绝整份记录时标准输出不应有任何业务内容，out=%q", label, out)
	}
	for _, banned := range []string{"总学分", "已满足", "成功"} {
		if strings.Contains(out, banned) {
			t.Fatalf("%s：标准输出不能出现 %q 类说明，out=%q", label, banned, out)
		}
	}
	for _, want := range []string{file, "内容损坏", "s1", "w1", "有效免修", "依据"} {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s：错误输出应点名文件/学生 s1/有效免修 w1/缺少依据（缺 %q），err=%q",
				label, want, errText)
		}
	}
	assertFileByteIdentical(t, file, raw, label+"：")
}

// TestCLIApprovedWaiverBlankBasisRejectsAllCommands 有效免修依据为空白时，
// 无论用哪条命令打开（只读核对、历史查看、列课程，还是本来会保存变更的
// 登记），都必须在读取阶段以退出码 2 拒绝：不输出任何业务结果，错误说明
// 记录文件内容损坏、s1 的有效免修 w1 缺少依据，原文件保持不变。
func TestCLIApprovedWaiverBlankBasisRejectsAllCommands(t *testing.T) {
	blanks := map[string]string{
		"空字符串":  "",
		"普通空格":  "     ",
		"制表符换行": "\t\n\r\n\t",
		"全角空格":  "　　　",
		"不换行空格": "     ",
		"混合空白":  " \t　 \n  \r　",
	}
	for name, basis := range blanks {
		t.Run(name, func(t *testing.T) {
			file, raw := writeDiskRecord(t, blankBasisRecord(basis, false))

			assertBlankBasisAccessRejected(t, file, raw,
				[]string{"check", "s1"}, "check 当事学生")
			assertBlankBasisAccessRejected(t, file, raw,
				[]string{"show", "s1"}, "show 当事学生")
			assertBlankBasisAccessRejected(t, file, raw,
				[]string{"list-courses"}, "list-courses")

			// 写入类命令：同样在读取阶段拒绝，不能覆盖原文件或新增业务记录。
			assertBlankBasisAccessRejected(t, file, raw,
				[]string{"student", "s9"}, "写入类命令 student")

			// 查询文件里根本不存在的学生也不能绕过：整份文件不可读。
			assertBlankBasisAccessRejected(t, file, raw,
				[]string{"check", "ghost"}, "check 其他学生")

			assertFileByteIdentical(t, file, raw, "全部访问后：")
		})
	}
}

// TestCLIBlankBasisApprovedNotMaskedByPassedEnrollment 即使该要求另有通过
// 修读（本来足以解释学分、满足要求），有效免修缺少依据仍必须以退出码 2
// 拒绝：不能出现总学分、要求已满足或通过修读来源的说明，文件原样保留。
func TestCLIBlankBasisApprovedNotMaskedByPassedEnrollment(t *testing.T) {
	file, raw := writeDiskRecord(t, blankBasisRecord(" 　\t \n ", true))

	out, errText, code := runCLI(t, file, "check", "s1")
	if code != exitFile {
		t.Fatalf("通过修读不能掩盖缺少依据的有效免修，code=%d out=%q err=%q",
			code, out, errText)
	}
	if out != "" {
		t.Fatalf("损坏记录不得输出任何核对结果，out=%q", out)
	}
	for _, banned := range []string{"总学分", "已满足", "通过修读"} {
		if strings.Contains(out, banned) {
			t.Fatalf("标准输出不能出现 %q，out=%q", banned, out)
		}
	}
	if !strings.Contains(errText, "内容损坏") || !strings.Contains(errText, "s1") ||
		!strings.Contains(errText, "w1") || !strings.Contains(errText, file) {
		t.Fatalf("错误应点名 %s 中 s1 的有效免修 w1 缺少依据，err=%q", file, errText)
	}

	// 本来会保存变更的登记也必须停在读取阶段，原文件（含通过修读）保留。
	out, errText, code = runCLI(t, file, "course-close", "c1")
	if code != exitFile || out != "" {
		t.Fatalf("写入类命令也应在读取阶段拒绝，code=%d out=%q err=%q",
			code, out, errText)
	}
	assertFileByteIdentical(t, file, raw, "登记尝试后：")
}

// TestCLIApprovedWaiverBasisWithWhitespaceAndTextOpensNormally 依据含有实际
// 文字、只是前后或中间带缩进、换行、全角空格与不换行空格时，记录必须正常
// 读取：check 以免修计 4 学分、要求已满足；show 展示的依据与保存原文一致
// （以 Go 引用形式出现，空白被转义但内容不缺）；只读访问不改文件。
func TestCLIApprovedWaiverBasisWithWhitespaceAndTextOpensNormally(t *testing.T) {
	basis := "\t\n  学科竞赛获奖证明　 \n  证书编号：001  \n  "
	d := blankBasisRecord(basis, false)
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("含实际文字的依据应正常核对，code=2? out=%q", out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：已满足") ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("应凭有效免修 w1 满足 r1、计 4 学分，out=%q", out)
	}
	if strings.Contains(out, "未满足要求：[") {
		t.Fatalf("r1 不应列为未满足，out=%q", out)
	}

	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 应成功，code=%d out=%q", code, show)
	}
	// 命令行以 %q 呈现依据，期望片段按同样方式引用，验证原文逐字符可查。
	want := fmt.Sprintf("免修 w1：要求 r1，依据 %s，状态：有效", fmt.Sprintf("%q", basis))
	if !strings.Contains(show, want) {
		t.Fatalf("show 应展示未被修剪的依据原文\nwant contains %q\nout=%q", want, show)
	}

	assertFileByteIdentical(t, file, raw, "check/show 之后：")
}

// TestCLIRejectedWaiverWithBlankBasisOpensNormally 已拒绝申请的依据为空白
// 时记录仍合法：拒绝状态与原因在 check/show 中照常可查、不获得学分，要求
// 由同记录中的通过修读满足；只读访问不改文件。
func TestCLIRejectedWaiverWithBlankBasisOpensNormally(t *testing.T) {
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Enrollments: []diskEnr{{
			ID: "e1", Student: "s1", Req: "r1", Term: "2024春",
			Result: "passed", ResultSeq: 1,
		}},
		NextResultSeq: 1,
		Waivers: []diskWaiver{
			{ID: "wr", Student: "s1", Req: "r1", Basis: " \t　 \n ",
				Status: "rejected", Reason: "免修依据为空"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("被拒绝的空白依据申请不应判坏文件，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("r1 应由通过修读满足、计 4 学分，out=%q", out)
	}
	if !strings.Contains(out, "免修 wr") || !strings.Contains(out, "免修依据为空") {
		t.Fatalf("被拒绝申请及原因应在核对中可查，out=%q", out)
	}
	if strings.Contains(out, "来源为有效免修") {
		t.Fatalf("被拒绝申请不能成为有效来源，out=%q", out)
	}
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 || !strings.Contains(show, "状态：已拒绝（免修依据为空）") {
		t.Fatalf("show 应保留被拒绝状态与原因，code=%d out=%q", code, show)
	}
	assertFileByteIdentical(t, file, raw, "只读访问之后：")
}

// TestCLIFirstTimeBlankBasisSubmissionStillBusinessRejection 用户首次正常
// 提交纯空白依据申请的行为不变：按业务规则以退出码 1 拒绝，申请内容与
// 拒绝原因保留在免修历史并落盘；随后重新打开是合法记录（退出码 0），
// 核对中可查到该拒绝记录，且不会被读取校验反过来判成内容损坏。
func TestCLIFirstTimeBlankBasisSubmissionStillBusinessRejection(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "数学", "4"},
		{"req", "s1", "r1", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 依据只含全角空格、不换行空格与制表换行：业务拒绝退出码 1。
	out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", " 　 \t \n ")
	if code != exitRejected {
		t.Fatalf("首次提交空白依据应按业务规则以退出码 1 拒绝，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(out, "已拒绝") {
		t.Fatalf("应说明申请已拒绝并记入免修历史，out=%q", out)
	}

	// 文件已保存，重新打开必须是合法记录：check 退出码 0，拒绝记录可查、
	// r1 未满足、学分为零。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("保存了被拒绝申请的文件应正常打开，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：0") || !strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("被拒绝的空白依据申请不获得学分，out=%q", out)
	}
	if !strings.Contains(out, "免修 w1") || !strings.Contains(out, "免修依据为空") {
		t.Fatalf("核对中应保留被拒绝申请与原因，out=%q", out)
	}

	// show 同样正常，状态为已拒绝；该文件绝不会被判成内容损坏。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 || !strings.Contains(show, "免修 w1") ||
		!strings.Contains(show, "状态：已拒绝（免修依据为空）") {
		t.Fatalf("show 应能查到被拒绝的空白依据申请，code=%d out=%q", code, show)
	}

	// 文件确实存在且为一份完整记录。
	if got, err := os.ReadFile(file); err != nil || !strings.Contains(string(got), `"status": "rejected"`) {
		t.Fatalf("被拒绝申请应已落盘保留，got=%q err=%v", got, err)
	}
}
