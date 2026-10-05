package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“revoke-waiver 按用户给出的完整学生编号与免修
// 编号确定撤销目标”。记录文件可以合法保存带前后空白（普通空格、制表符、
// 全角空格）的编号，查询能把它们区分，撤销也必须如此：
//   - 撤销 " s1 " 的 w1 只命中本人的申请，"s1" 的免修状态、学分与来源
//     保持原样；
//   - 同一学生名下 "w1" 与 " w1 " 分别处理，撤销一份不影响另一份，撤销
//     提示使用实际命中的原编号；
//   - 完整编号找不到时按业务拒绝（退出码 1），明确报告学生不存在或该
//     学生名下不存在该免修，不输出撤销成功的提示，不借用去空白后能碰上
//     的记录，记录文件逐字节保持原样；
//   - 含前后空白的编号是合法记录，不能判为文件损坏（退出码 2）。

// revokeWhitespaceCLIRecord 构造合法记录：学生 "s1" 与 " s1 " 各自的
// 要求 r1 指向 4 学分课程 c1，名下各有一份编号 w1 的有效免修，两人都
// 没有任何修读。
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
			{ID: "w1", Student: "s1", Req: "r1", Basis: "s1的获奖证明", Status: "approved"},
			{ID: "w1", Student: " s1 ", Req: "r1", Basis: " s1 的获奖证明", Status: "approved"},
		},
	}
}

// TestCLIRevokeWhitespaceStudentIDHitsExactOwner 给 " s1 " 撤销 w1：
// 只有这名学生的申请被撤销，本人要求重新未满足、失去学分，免修历史保留
// 原要求、原依据并记录撤销状态与原因；"s1" 的免修状态、学分和来源保持
// 原样。
func TestCLIRevokeWhitespaceStudentIDHitsExactOwner(t *testing.T) {
	file, _ := writeDiskRecord(t, revokeWhitespaceCLIRecord())

	// 撤销提示使用实际命中的原编号 w1。
	out, errText, code := runCLI(t, file, "revoke-waiver", " s1 ", "w1", "材料无法核实")
	if code != 0 || !strings.Contains(out, "免修 w1 已撤销") {
		t.Fatalf("撤销 \" s1 \" 的 w1 应成功，code=%d out=%q err=%q", code, out, errText)
	}

	// " s1 " 本人：没有通过修读，0 学分、r1 未满足，核对应列出已撤销的 w1。
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") ||
		!strings.Contains(out, "已撤销免修：[w1]") {
		t.Fatalf("\" s1 \" 撤销后应为 0 学分、r1 未满足且列出已撤销 w1，code=%d out=%q",
			code, out)
	}
	// 免修历史保留原编号、原要求、原依据与撤销原因。
	show, _, _ := runCLI(t, file, "show", " s1 ")
	if !strings.Contains(show, `免修 w1：要求 r1，依据 " s1 的获奖证明"，状态：已撤销`) ||
		!strings.Contains(show, "材料无法核实") {
		t.Fatalf("\" s1 \" 的免修历史应保留原要求、原依据与撤销原因，out=%q", show)
	}

	// "s1" 保持原样：w1 仍有效、4 学分、来源为本人有效免修。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("\"s1\" 应仍为 4 学分且由本人 w1 满足，code=%d out=%q", code, out)
	}
	if strings.Contains(out, "已撤销免修") {
		t.Fatalf("\"s1\" 名下不应出现已撤销免修，out=%q", out)
	}
	show, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(show, `免修 w1：要求 r1，依据 "s1的获奖证明"，状态：有效`) {
		t.Fatalf("\"s1\" 的 w1 应保持有效且依据不变，out=%q", show)
	}
}

// TestCLIRevokeWhitespaceWaiverIDIndependent 同一学生名下 "w1" 与 " w1 "
// 是指向不同要求的两份有效免修：撤销 " w1 " 只处理带空格的那份，提示
// 使用实际命中的原编号；"w1" 继续有效，另一份要求的满足情况与学分不
// 受影响。
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
			{ID: "w1", Student: "s1", Req: "r1", Basis: "竞赛获奖", Status: "approved"},
			{ID: " w1 ", Student: "s1", Req: "r2", Basis: "外校修读证明", Status: "approved"},
		},
	}
	file, _ := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "revoke-waiver", "s1", " w1 ", "材料无法核实")
	if code != 0 || !strings.Contains(out, "免修  w1  已撤销") {
		t.Fatalf("撤销 \" w1 \" 应命中带空格的原编号，code=%d out=%q err=%q",
			code, out, errText)
	}

	// r2 重新未满足，总学分只剩 r1 的 4 学分，来源仍是有效免修 w1。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w1") ||
		!strings.Contains(out, "未满足要求：[r2]") ||
		!strings.Contains(out, "已撤销免修：[ w1 ]") {
		t.Fatalf("撤销 \" w1 \" 后应只剩 r1 的 4 学分、r2 未满足，code=%d out=%q",
			code, out)
	}
	// 两份免修各自保留原编号、原要求、原依据与状态。
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, `免修 w1：要求 r1，依据 "竞赛获奖"，状态：有效`) ||
		!strings.Contains(show, `免修  w1 ：要求 r2，依据 "外校修读证明"，状态：已撤销`) {
		t.Fatalf("两份免修应各自保留原编号、要求、依据与状态，out=%q", show)
	}
}

// TestCLIRevokeWhitespaceIDRejected 完整编号找不到时按业务拒绝：
// 退出码 1，明确报告学生不存在或该学生名下不存在该免修，不输出撤销
// 成功的提示；即使去掉空白能碰上另一条记录也不借用、不改写；普通空格、
// 制表符、全角空格一样不能被忽略；记录文件逐字节保持原样。
func TestCLIRevokeWhitespaceIDRejected(t *testing.T) {
	cases := []struct {
		label   string
		args    []string
		wantErr []string
	}{
		// 学生编号带前后空白：记录中是 "s1" 与 " s1 "，这些都应报学生不存在。
		{"学生前导空格", []string{"revoke-waiver", " s1", "w1", "原因"}, []string{"学生", "不存在"}},
		{"学生尾随空格", []string{"revoke-waiver", "s1  ", "w1", "原因"}, []string{"学生", "不存在"}},
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
			file, raw := writeDiskRecord(t, revokeWhitespaceCLIRecord())
			out, errText, code := runCLI(t, file, tc.args...)
			if code != exitRejected {
				t.Fatalf("%s：应业务拒绝（退出码 1），code=%d out=%q err=%q",
					tc.label, code, out, errText)
			}
			if strings.Contains(out, "已撤销") {
				t.Fatalf("%s：被拒绝时不应输出撤销成功的提示，out=%q", tc.label, out)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(errText, want) {
					t.Fatalf("%s：错误输出应包含 %q，err=%q", tc.label, want, errText)
				}
			}
			assertFileByteIdentical(t, file, raw, tc.label+"：")

			// 两名学生的 w1 都应保持有效。
			for _, student := range []string{"s1", " s1 "} {
				show, _, _ := runCLI(t, file, "show", student)
				if !strings.Contains(show, "状态：有效") ||
					strings.Contains(show, "状态：已撤销") {
					t.Fatalf("%s：拒绝后学生 %s 的 w1 应保持有效，out=%q",
						tc.label, student, show)
				}
			}
		})
	}
}

// TestCLIRevokeWhitespaceIdempotentKeepsFirstReason 准确命中带空白编号的
// 免修并撤销后，重复撤销返回原结果且文件不再改写，后来给出的原因不覆盖
// 首次撤销原因；撤销后有通过记录时仍只得一份学分、来源恢复为最先提交
// 的通过记录。
func TestCLIRevokeWhitespaceIdempotentKeepsFirstReason(t *testing.T) {
	d := &diskRecord{
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
		Enrollments: []diskEnr{
			// " s1 " 有两次通过：撤销免修后来源应恢复为最先提交的 e2。
			{ID: "e2", Student: " s1 ", Req: "r1", Term: "2024春", Result: "passed", ResultSeq: 1},
			{ID: "e1", Student: " s1 ", Req: "r1", Term: "2024秋", Result: "passed", ResultSeq: 2},
		},
		Waivers: []diskWaiver{
			{ID: "w1", Student: "s1", Req: "r1", Basis: "s1的获奖证明", Status: "approved"},
			{ID: "w1", Student: " s1 ", Req: "r1", Basis: " s1 的获奖证明", Status: "approved"},
		},
		NextResultSeq: 2,
	}
	file, _ := writeDiskRecord(t, d)

	if _, errText, code := runCLI(t, file, "revoke-waiver", " s1 ", "w1", "首次撤销：材料存疑"); code != 0 {
		t.Fatalf("首次撤销应成功，code=%d err=%q", code, errText)
	}
	// 有通过记录：继续满足、只计一份 4 学分，来源恢复为最先提交的 e2。
	out, _, code := runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e2") {
		t.Fatalf("撤销后有通过记录应继续满足且来源恢复为 e2，code=%d out=%q", code, out)
	}
	before := mustReadRecord(t, file)

	// 重复撤销：返回原结果，文件不再改写，首因不被后来给出的原因覆盖。
	out, _, code = runCLI(t, file, "revoke-waiver", " s1 ", "w1", "后来给出的另一原因")
	if code != 0 || !strings.Contains(out, "已是撤销状态") {
		t.Fatalf("重复撤销应幂等返回原结果，code=%d out=%q", code, out)
	}
	assertRecordUnchanged(t, file, before, "幂等重复撤销：")
	show, _, _ := runCLI(t, file, "show", " s1 ")
	if !strings.Contains(show, "首次撤销：材料存疑") ||
		strings.Contains(show, "后来给出的另一原因") {
		t.Fatalf("首次撤销原因不应被后来的原因覆盖，out=%q", show)
	}

	// 另一名学生的有效免修、学分与来源始终不变。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("\"s1\" 的状态不应受影响，code=%d out=%q", code, out)
	}
}

// TestCLIRevokeWhitespaceRejectedStillRejected 完整编号命中的若是已拒绝
// 申请，仍不能撤销（退出码 1）；不能借用另一名学生同号的有效免修，
// 记录文件保持原样。
func TestCLIRevokeWhitespaceRejectedStillRejected(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students: []diskStudent{
			{ID: "s1"},
			{ID: " s1 "},
		},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
		},
		Waivers: []diskWaiver{
			{ID: "w1", Student: "s1", Req: "r1", Basis: "竞赛获奖", Status: "approved"},
			{ID: "w1", Student: " s1 ", Req: "rX", Basis: "材料",
				Status: "rejected", Reason: "目标要求 rX 不存在或不属于该学生"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "revoke-waiver", " s1 ", "w1", "撤销原因")
	if code != exitRejected || !strings.Contains(errText, "已被拒绝") {
		t.Fatalf("撤销已拒绝申请应退出码 1 并说明已被拒绝，code=%d out=%q err=%q",
			code, out, errText)
	}
	if strings.Contains(out, "已撤销") {
		t.Fatalf("被拒绝时不应输出撤销成功的提示，out=%q", out)
	}
	assertFileByteIdentical(t, file, raw, "撤销已拒绝申请：")
	// 另一名学生的同号有效免修保持有效、学分不变。
	check, _, _ := runCLI(t, file, "check", "s1")
	if !strings.Contains(check, "总学分：4") || !strings.Contains(check, "来源为有效免修 w1") {
		t.Fatalf("s1 的有效 w1 不应受影响，out=%q", check)
	}
	show, _, _ := runCLI(t, file, "show", " s1 ")
	if !strings.Contains(show, "状态：已拒绝") {
		t.Fatalf("\" s1 \" 的已拒绝 w1 应保持原样，out=%q", show)
	}
}
