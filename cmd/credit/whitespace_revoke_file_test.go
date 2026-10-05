package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“revoke-waiver 按用户给出的完整学生编号与免修
// 编号确定撤销目标”。记录文件可以合法保存带前后空白（普通空格、制表符、
// 全角空格）的编号，查询能把它们区分，撤销也必须如此：
//   - 两名学生 "s1" 与 " s1 " 各有一份有效免修 w1 时，指定 " s1 " 撤销
//     w1 只撤销这名学生的申请，"s1" 的免修状态、学分和来源保持原样；
//   - 同一学生名下 "w1" 与 " w1 " 两份有效免修分别指向不同要求时，
//     指定 " w1 " 只处理带空格的那份，另一份继续有效；
//   - 撤销提示使用实际命中的原编号，免修历史保留原要求与原依据并记录
//     撤销状态与原因；
//   - 完整学生编号不存在时报学生不存在，学生存在但名下没有完整免修编号
//     时报该学生名下不存在该免修：退出码 1，标准错误点名对应对象，标准
//     输出没有撤销成功提示，记录文件逐字节保持原样；即使去掉空白能碰上
//     另一条记录也不借用、不改写编号、不合并申请、不新增免修历史；
//   - 含前后空白的编号是合法记录，不能判为文件损坏（退出码 2）；
//   - 准确命中后沿用现有学分规则与撤销规则：无通过修读则要求重新未满足，
//     有通过修读则仍只得一份课程学分且来源恢复为最先提交的通过记录；
//     重复撤销返回原结果且首因不被覆盖；已拒绝申请不能撤销。

// revokeWhitespaceCLIRecord 构造合法记录：学生 "s1" 与 " s1 " 各自的
// 要求 r1 指向 4 学分课程 c1，名下各有一份有效免修 w1，均无通过修读。
func revokeWhitespaceCLIRecord() *diskRecord {
	return &diskRecord{
		Version: 1,
		Courses: []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students: []diskStudent{
			{ID: "s1"},
			{ID: " s1 "},
		},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
			{ID: "r1", Student: " s1 ", Course: "c1"},
		},
		Waivers: []diskWaiver{
			{ID: "w1", Student: "s1", Req: "r1", Basis: "s1 的竞赛获奖", Status: "approved"},
			{ID: "w1", Student: " s1 ", Req: "r1", Basis: " s1 的竞赛材料 ", Status: "approved"},
		},
	}
}

// TestCLIRevokeWhitespaceStudentIDHitsExactOwner 指定 " s1 " 撤销 w1：
// 只有这名学生的申请被撤销，其要求重新未满足、失去这份免修学分，撤销
// 提示与历史使用实际命中的原编号并保留原依据与撤销原因；"s1" 的免修
// 状态、学分和来源保持原样。
func TestCLIRevokeWhitespaceStudentIDHitsExactOwner(t *testing.T) {
	file, _ := writeDiskRecord(t, revokeWhitespaceCLIRecord())

	out, errText, code := runCLI(t, file, "revoke-waiver", " s1 ", "w1", "材料无法核实")
	if code != 0 || errText != "" {
		t.Fatalf("撤销 \" s1 \" 的 w1 应成功，code=%d out=%q err=%q", code, out, errText)
	}
	// 撤销成功提示必须出现，且使用实际命中的原编号与原依据（依据原文两端
	// 带空格，%q 渲染后仍可见）。
	if !strings.Contains(out, "免修 w1 已撤销") ||
		!strings.Contains(out, `" s1 的竞赛材料 "`) {
		t.Fatalf("撤销提示应使用实际命中申请的原编号与原依据，out=%q", out)
	}

	// " s1 " 本人：无通过修读，r1 重新未满足、0 学分，已撤销 w1 列出。
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") ||
		!strings.Contains(out, "已撤销免修：[w1]") ||
		strings.Contains(out, "来源为有效免修") {
		t.Fatalf("\" s1 \" 撤销后应失去这份免修学分，code=%d out=%q", code, out)
	}
	// 历史保留原要求、原依据、撤销状态与原因。
	show, _, code := runCLI(t, file, "show", " s1 ")
	if code != 0 || !strings.Contains(show, `免修 w1：要求 r1，依据 " s1 的竞赛材料 "，状态：已撤销（材料无法核实）`) {
		t.Fatalf("show 应保留 \" s1 \" 的原要求、原依据与撤销原因，code=%d show=%q", code, show)
	}

	// "s1" 保持原样：w1 仍有效、r1 满足、4 学分，没有撤销历史。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w1") ||
		strings.Contains(out, "未满足要求：[") ||
		strings.Contains(out, "已撤销免修") {
		t.Fatalf("\"s1\" 的免修状态、学分和来源应保持原样，code=%d out=%q", code, out)
	}
	show, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(show, `免修 w1：要求 r1，依据 "s1 的竞赛获奖"，状态：有效`) {
		t.Fatalf("\"s1\" 的 w1 应继续有效且依据不变，show=%q", show)
	}
}

// TestCLIRevokeWhitespaceWaiverIDIndependent 同一学生名下 "w1" 与
// " w1 " 两份有效免修分别指向不同要求（不同学分课程）：撤销 " w1 "
// 只处理带空格的那份，其要求重新未满足；"w1" 继续有效，另一项要求与
// 其学分不受影响，提示使用带空格的原编号。
func TestCLIRevokeWhitespaceWaiverIDIndependent(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
			{ID: "r2", Student: "s1", Course: "c2"},
		},
		Waivers: []diskWaiver{
			{ID: "w1", Student: "s1", Req: "r1", Basis: "r1 的竞赛获奖", Status: "approved"},
			{ID: " w1 ", Student: "s1", Req: "r2", Basis: " r2 的外校修读 ", Status: "approved"},
		},
	}
	file, _ := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "revoke-waiver", "s1", " w1 ", "材料无法核实")
	if code != 0 || errText != "" {
		t.Fatalf("撤销 \" w1 \" 应成功，code=%d out=%q err=%q", code, out, errText)
	}
	// 提示必须使用实际命中的带空格原编号。
	if !strings.Contains(out, "免修  w1  已撤销") ||
		!strings.Contains(out, `" r2 的外校修读 "`) {
		t.Fatalf("撤销提示应使用带空格的原编号与原依据，out=%q", out)
	}

	// r2 无通过修读：重新未满足，3 学分消失；r1 仍由 w1 满足、4 学分。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w1") ||
		!strings.Contains(out, "未满足要求：[r2]") ||
		!strings.Contains(out, "已撤销免修：[ w1 ]") {
		t.Fatalf("只应撤销 \" w1 \" 所在的 r2，r1 的 w1 继续有效，code=%d out=%q", code, out)
	}
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, `免修 w1：要求 r1，依据 "r1 的竞赛获奖"，状态：有效`) ||
		!strings.Contains(show, `免修  w1 ：要求 r2，依据 " r2 的外校修读 "，状态：已撤销（材料无法核实）`) {
		t.Fatalf("两份免修应各自保留原编号、原要求、原依据与状态，show=%q", show)
	}
}

// TestCLIRevokeWhitespaceRestoresEarliestPass 命中带空白编号的免修后，
// 目标要求已有通过修读时仍只得一份课程学分，来源恢复为最先提交的通过
// 记录；没有通过修读的另一名学生不受影响。
func TestCLIRevokeWhitespaceRestoresEarliestPass(t *testing.T) {
	d := revokeWhitespaceCLIRecord()
	// " s1 " 的 r1 两次通过：e1 先、e2 后。
	d.Enrollments = []diskEnr{
		{ID: "e1", Student: " s1 ", Req: "r1", Term: "2024春", Result: "passed", ResultSeq: 1},
		{ID: "e2", Student: " s1 ", Req: "r1", Term: "2024秋", Result: "passed", ResultSeq: 2},
	}
	d.NextResultSeq = 2
	file, _ := writeDiskRecord(t, d)

	if _, errText, code := runCLI(t, file, "revoke-waiver", " s1 ", "w1", "材料无法核实"); code != 0 {
		t.Fatalf("撤销应成功，code=%d err=%q", code, errText)
	}
	out, _, code := runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") ||
		strings.Contains(out, "未满足要求：[") ||
		!strings.Contains(out, "已撤销免修：[w1]") {
		t.Fatalf("有通过修读时撤销免修应继续满足且来源恢复为最先的 e1，code=%d out=%q", code, out)
	}
	// "s1" 的免修来源与学分不受影响。
	out, _, _ = runCLI(t, file, "check", "s1")
	if !strings.Contains(out, "总学分：4") || !strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("\"s1\" 的学分与来源应保持原样，out=%q", out)
	}
}

// TestCLIRevokeWhitespaceIDRejected 完整编号找不到时按业务拒绝：退出码 1，
// 标准错误点名对应对象，标准输出没有撤销成功提示；即使去掉空白能碰上
// 另一条记录也不借用、不改写编号、不合并申请、不新增免修历史；普通空格、
// 制表符、全角空格一样不能被忽略；记录文件逐字节保持原样。
func TestCLIRevokeWhitespaceIDRejected(t *testing.T) {
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Waivers: []diskWaiver{
			{ID: "w1", Student: "s1", Req: "r1", Basis: "竞赛获奖", Status: "approved"},
		},
	}

	cases := []struct {
		label   string
		args    []string
		wantErr []string
	}{
		// 学生编号带前后空白：文件中只有 "s1"，应明确报告学生不存在。
		{"学生前导空格", []string{"revoke-waiver", " s1", "w1", "原因"}, []string{"学生", "不存在"}},
		{"学生尾随空格", []string{"revoke-waiver", "s1 ", "w1", "原因"}, []string{"学生", "不存在"}},
		{"学生制表符", []string{"revoke-waiver", "\ts1\t", "w1", "原因"}, []string{"学生", "不存在"}},
		{"学生全角空格", []string{"revoke-waiver", "　s1　", "w1", "原因"}, []string{"学生", "不存在"}},
		// 学生存在但免修编号带前后空白：应明确报告该学生名下不存在该免修。
		{"免修前导空格", []string{"revoke-waiver", "s1", " w1", "原因"}, []string{"s1", "不存在"}},
		{"免修尾随空格", []string{"revoke-waiver", "s1", "w1 ", "原因"}, []string{"s1", "不存在"}},
		{"免修制表符", []string{"revoke-waiver", "s1", "\tw1\t", "原因"}, []string{"s1", "不存在"}},
		{"免修全角空格", []string{"revoke-waiver", "s1", "　w1　", "原因"}, []string{"s1", "不存在"}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			file, raw := writeDiskRecord(t, d)
			out, errText, code := runCLI(t, file, tc.args...)
			if code != exitRejected {
				t.Fatalf("%s：应业务拒绝（退出码 1），code=%d out=%q err=%q",
					tc.label, code, out, errText)
			}
			if strings.Contains(out, "已撤销") {
				t.Fatalf("%s：被拒绝时不应输出撤销成功提示，out=%q", tc.label, out)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(errText, want) {
					t.Fatalf("%s：错误输出应包含 %q，err=%q", tc.label, want, errText)
				}
			}
			assertFileByteIdentical(t, file, raw, tc.label+"：")

			// 借用去空白后能碰上的 "s1"/w1 不应被撤销。
			show, _, _ := runCLI(t, file, "show", "s1")
			if !strings.Contains(show, `免修 w1：要求 r1，依据 "竞赛获奖"，状态：有效`) {
				t.Fatalf("%s：原有效免修应保持原样，show=%q", tc.label, show)
			}
		})
	}
}

// TestCLIRevokeWhitespaceNoMergeAcrossIDs 同一学生名下只有带空格的
// " w1 " 时撤销 "w1"：必须按该学生名下不存在该免修拒绝，不能借用或
// 合并到 " w1 "；反之撤销存在的 " w1 " 后再撤销 "w1" 仍报不存在，
// 且带空格编号的历史原样保留、不新增任何免修历史。
func TestCLIRevokeWhitespaceNoMergeAcrossIDs(t *testing.T) {
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Waivers: []diskWaiver{
			{ID: " w1 ", Student: "s1", Req: "r1", Basis: "带空格编号的依据", Status: "approved"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "revoke-waiver", "s1", "w1", "原因")
	if code != exitRejected || !strings.Contains(errText, "s1") ||
		!strings.Contains(errText, "名下不存在免修 w1") {
		t.Fatalf("只有 \" w1 \" 时撤销 \"w1\" 应按不存在拒绝，code=%d out=%q err=%q",
			code, out, errText)
	}
	if strings.Contains(out, "已撤销") {
		t.Fatalf("被拒绝时不应输出撤销成功提示，out=%q", out)
	}
	assertFileByteIdentical(t, file, raw, "按不存在拒绝：")

	// 准确撤销带空格的那份成功后，撤销不带空格的 "w1" 仍应按不存在拒绝，
	// 不能复用已撤销的 " w1 " 充当幂等命中。
	if _, _, code := runCLI(t, file, "revoke-waiver", "s1", " w1 ", "首次原因"); code != 0 {
		t.Fatal("准确撤销 \" w1 \" 应成功")
	}
	_, errText, code = runCLI(t, file, "revoke-waiver", "s1", "w1", "原因")
	if code != exitRejected || !strings.Contains(errText, "名下不存在免修 w1") {
		t.Fatalf("撤销 \" w1 \" 后再撤销 \"w1\" 仍应报不存在，code=%d err=%q", code, errText)
	}
	show, _, _ := runCLI(t, file, "show", "s1")
	if strings.Count(show, "免修") != 1 ||
		!strings.Contains(show, `免修  w1 ：要求 r1，依据 "带空格编号的依据"，状态：已撤销（首次原因）`) {
		t.Fatalf("历史应只有带空格原编号的一条且保留首因，show=%q", show)
	}
}

// TestCLIRevokeWhitespaceIdempotentKeepsFirstReason 准确命中带空白编号
// 的免修并撤销后，重复撤销返回原结果、退出码 0、文件不再改写；后来给出
// 的原因不会覆盖首次撤销原因。
func TestCLIRevokeWhitespaceIdempotentKeepsFirstReason(t *testing.T) {
	file, _ := writeDiskRecord(t, revokeWhitespaceCLIRecord())

	if _, errText, code := runCLI(t, file, "revoke-waiver", " s1 ", "w1", "首次撤销：材料存疑"); code != 0 {
		t.Fatalf("首次撤销应成功，code=%d err=%q", code, errText)
	}
	before := mustReadRecord(t, file)

	// 后来的原因（含空白原因）都不能覆盖首因；输出“已是撤销状态”的原结果。
	for _, reason := range []string{"第二次原因", "   "} {
		out, _, code := runCLI(t, file, "revoke-waiver", " s1 ", "w1", reason)
		if code != 0 || !strings.Contains(out, "已是撤销状态") {
			t.Fatalf("重复撤销应幂等返回原结果，reason=%q code=%d out=%q", reason, code, out)
		}
		assertRecordUnchanged(t, file, before, "重复撤销 "+reason+"：")
	}
	show, _, _ := runCLI(t, file, "show", " s1 ")
	if !strings.Contains(show, "状态：已撤销（首次撤销：材料存疑）") {
		t.Fatalf("首次撤销原因不应被后来的原因覆盖，show=%q", show)
	}
	if strings.Count(show, "免修 w1：") != 1 {
		t.Fatalf("重复撤销不应新增免修历史，show=%q", show)
	}

	// 用修剪后会撞上的另一名学生 "s1" 撤销 w1：必须报学生不存在，且
	// "s1" 的有效免修保持原样。
	_, errText, code := runCLI(t, file, "revoke-waiver", "s1 ", "w1", "原因")
	if code != exitRejected || !strings.Contains(errText, "学生") ||
		!strings.Contains(errText, "不存在") {
		t.Fatalf("用 \"s1 \" 撤销应报学生不存在，code=%d err=%q", code, errText)
	}
	show, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "状态：有效") {
		t.Fatalf("\"s1\" 的有效免修不应受影响，show=%q", show)
	}
}

// TestCLIRevokeWhitespaceRejectedCannotRevoke 已拒绝的带空白编号申请不能
// 撤销：退出码 1，原拒绝状态与原因保留；同文件中的其他有效免修不受影响。
func TestCLIRevokeWhitespaceRejectedCannotRevoke(t *testing.T) {
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Waivers: []diskWaiver{
			{ID: " w1 ", Student: " s1 ", Req: "rX", Basis: "依据",
				Status: "rejected", Reason: "目标要求 rX 不存在或不属于该学生"},
			{ID: "w1", Student: "s1", Req: "r1", Basis: "s1 的依据", Status: "approved"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "revoke-waiver", " s1 ", " w1 ", "新的撤销原因")
	if code != exitRejected || !strings.Contains(errText, "已被拒绝") {
		t.Fatalf("已拒绝申请不能撤销，code=%d out=%q err=%q", code, out, errText)
	}
	if strings.Contains(out, "已撤销") {
		t.Fatalf("被拒绝时不应输出撤销成功提示，out=%q", out)
	}
	assertFileByteIdentical(t, file, raw, "拒绝撤销已拒绝申请：")
	show, _, _ := runCLI(t, file, "show", " s1 ")
	if !strings.Contains(show, `免修  w1 ：要求 rX，依据 "依据"，状态：已拒绝（目标要求 rX 不存在或不属于该学生）`) {
		t.Fatalf("已拒绝申请的状态与首因应原样保留，show=%q", show)
	}
	show, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(show, `免修 w1：要求 r1，依据 "s1 的依据"，状态：有效`) {
		t.Fatalf("\"s1\" 的有效免修不应受影响，show=%q", show)
	}
}
