package main

import (
	"fmt"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“读取已有记录时，有效免修的依据校验与正常申请
// 规则一致”。带空白依据的有效免修不可能经正常申请流程落入文件（正常提交
// 空白依据会被拒绝并保留为已拒绝申请），所以用例直接写出结构完整、可解析
// 的记录文件，每条断言都重新打开进程访问它：
//   - 有效免修的依据为空或只含空白字符（空格、制表符、换行、全角空格、
//     不换行空格及混合）：任何命令都以退出码 2 结束，标准输出没有任何
//     业务结果，错误说明记录文件内容损坏并点名学生、免修编号与文件，
//     原文件逐字节保留；通过修读不能绕过校验；
//   - 依据含实际文字、带缩进与换行的记录正常读取，查看历史可见原内容；
//   - 首次正常提交空白依据的行为不变：按业务规则拒绝（退出码 1），
//     申请与原因保留在免修历史中。

// blankBasisDiskRecord 构造结构完整、引用齐全的记录：s1 的 r1 指向 4 学分
// 课程 c1，名下有一份状态为有效、依据为 basis 的免修 w1。withPass 时再附
// 一条已通过修读，用于证明通过记录不能绕过依据校验。
func blankBasisDiskRecord(basis string, withPass bool) *diskRecord {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
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

// assertBlankBasisAccessRejected 用指定命令访问含空白依据有效免修的文件：
// 必须退出码 2、stdout 没有任何业务内容，stderr 说明内容损坏并点名文件、
// 学生与免修编号，原文件逐字节保留。
func assertBlankBasisAccessRejected(t *testing.T, file string, raw []byte,
	args []string, label string) {
	t.Helper()
	out, errText, code := runCLI(t, file, args...)
	if code != exitFile {
		t.Fatalf("%s：访问缺依据的有效免修记录应退出码 %d，code=%d out=%q err=%q",
			label, exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("%s：拒绝整份记录时标准输出不应出现任何业务结果，out=%q", label, out)
	}
	for _, want := range []string{file, "内容损坏", "s1", "w1", "有效免修", "依据"} {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s：错误输出应包含 %q（文件/学生/免修编号/依据说明），err=%q",
				label, want, errText)
		}
	}
	assertFileByteIdentical(t, file, raw, label+"：")
}

// TestCLIBlankBasisApprovedWaiverRejectsAllCommands 文件完整可解析，但 s1 的
// 有效免修 w1 依据只含空白字符（混合半角空格、制表符、换行、全角空格与
// 不换行空格）：check、show 乃至写入类命令都必须退出码 2，不输出总学分、
// 要求已满足或登记成功信息，错误点名损坏要素，文件原样保留。
func TestCLIBlankBasisApprovedWaiverRejectsAllCommands(t *testing.T) {
	file, raw := writeDiskRecord(t, blankBasisDiskRecord(" \t　\n \r　 ", false))

	assertBlankBasisAccessRejected(t, file, raw, []string{"check", "s1"}, "check 当事学生")
	assertBlankBasisAccessRejected(t, file, raw, []string{"show", "s1"}, "show 当事学生")
	assertBlankBasisAccessRejected(t, file, raw, []string{"list-courses"}, "list-courses")
	// 写入类命令同样应在读取阶段拒绝：不能报告登记成功，更不能覆盖原文件。
	assertBlankBasisAccessRejected(t, file, raw, []string{"student", "s9"}, "写入类命令 student")
	assertBlankBasisAccessRejected(t, file, raw,
		[]string{"waiver", "s1", "r1", "w2", "补充依据"}, "写入类命令 waiver")

	assertFileByteIdentical(t, file, raw, "全部访问后：")
}

// TestCLIBlankBasisNotMaskedByPassedEnrollment 即使目标要求另有通过修读
// （本来足以满足要求），有效免修缺少实际依据仍必须以退出码 2 拒绝整份
// 记录：不能拿着通过结果输出学分核对，文件原样保留。
func TestCLIBlankBasisNotMaskedByPassedEnrollment(t *testing.T) {
	file, raw := writeDiskRecord(t, blankBasisDiskRecord("　 ", true))

	out, errText, code := runCLI(t, file, "check", "s1")
	if code != exitFile {
		t.Fatalf("通过修读不能绕过有效免修的依据校验，code=%d out=%q err=%q", code, out, errText)
	}
	if out != "" || strings.Contains(out, "总学分") || strings.Contains(out, "已满足") {
		t.Fatalf("损坏记录不得输出任何学分核对结果，out=%q", out)
	}
	if !strings.Contains(errText, "内容损坏") || !strings.Contains(errText, "s1") ||
		!strings.Contains(errText, "w1") || !strings.Contains(errText, file) {
		t.Fatalf("错误应说明内容损坏并点名 s1 的 w1 与记录文件，err=%q", errText)
	}
	assertFileByteIdentical(t, file, raw, "带通过修读的损坏文件：")
}

// TestCLIBlankBasisVariantsRejected 空字符串、纯半角空格、纯全角空格、
// 纯不换行空格等各种空白依据逐一验证：都按内容损坏拒绝。
func TestCLIBlankBasisVariantsRejected(t *testing.T) {
	for name, basis := range map[string]string{
		"空字符串":  "",
		"半角空格":  "   ",
		"全角空格":  "　　",
		"不换行空格": "  ",
		"制表符换行": "\t\n",
	} {
		t.Run(name, func(t *testing.T) {
			file, raw := writeDiskRecord(t, blankBasisDiskRecord(basis, false))
			assertBlankBasisAccessRejected(t, file, raw, []string{"check", "s1"}, "check")
		})
	}
}

// TestCLITextBasisWithWhitespaceLoadsAndShowsVerbatim 依据含有实际文字、
// 带缩进与换行时记录正常读取：核对由该免修满足，查看历史可见依据原内容，
// 读取与核对都不修剪、不改写已保存的材料。
func TestCLITextBasisWithWhitespaceLoadsAndShowsVerbatim(t *testing.T) {
	basis := "  课程证明：\n\t外校同层次课程已修毕\n  附成绩单编号 2024-018  "
	file, raw := writeDiskRecord(t, blankBasisDiskRecord(basis, false))

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("含实际文字的依据应正常核对，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") || !strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("r1 应由有效免修 w1 满足并计 4 学分，out=%q", out)
	}
	show, _, code := runCLI(t, file, "show", "s1")
	// show 以 %q 展示依据：换行与缩进以转义形式出现，内容与原文逐字符对应。
	if code != 0 || !strings.Contains(show, fmt.Sprintf("%q", basis)) {
		t.Fatalf("查看免修历史应能查到依据原内容（含缩进与换行），code=%d out=%q", code, show)
	}
	assertFileByteIdentical(t, file, raw, "只读访问之后：")
}

// TestCLIFirstBlankBasisApplicationStillBusinessRejected 正常提交空白依据的
// 首次申请行为不变：按业务规则拒绝（退出码 1），申请与拒绝原因保留在免修
// 历史中并落盘，后续核对可查到该被拒绝申请。
func TestCLIFirstBlankBasisApplicationStillBusinessRejected(t *testing.T) {
	file, _ := writeDiskRecord(t, &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
	})

	out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "  ")
	if code != exitRejected {
		t.Fatalf("首次提交空白依据应按业务规则拒绝（退出码 1），code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "已拒绝") || !strings.Contains(out, "免修依据为空") {
		t.Fatalf("拒绝输出应说明依据为空，out=%q", out)
	}
	// 被拒绝的申请已记入历史并落盘：再次核对可见，且文件不是损坏记录。
	check, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("含被拒绝空白依据申请的文件应正常核对，code=%d out=%q", code, check)
	}
	if !strings.Contains(check, "被拒绝的免修：") || !strings.Contains(check, "免修 w1") ||
		!strings.Contains(check, "免修依据为空") {
		t.Fatalf("核对应列出被拒绝申请及原因，out=%q", check)
	}
	if !strings.Contains(check, "总学分：0") || !strings.Contains(check, "未满足要求：[r1]") {
		t.Fatalf("被拒绝的申请不应获得学分，out=%q", check)
	}
}
