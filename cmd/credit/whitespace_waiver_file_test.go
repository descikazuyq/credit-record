package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“waiver 按用户传入的完整学生编号、要求编号和
// 免修编号处理申请”。记录文件可以合法保存带前后空白（普通空格、制表符、
// 全角空格 U+3000）的编号，成绩提交与免修撤销已按完整编号操作，申请
// 免修也必须如此：
//   - 给 " s1 " 提交依据含实际文字的新申请，只满足 " s1 " 本人同号要求
//     指向的课程并获得本人课程学分；提示、show 中的历史、check 中的来源
//     都保留申请实际使用的编号，另一名学生的学分、要求与申请保持原样；
//   - 同一学生名下 "r1" 与 " r1 " 必须区分，新申请只替代完整编号对应的
//     要求；"w1" 与 " w1 " 是不同申请编号，分别保留，幂等与冲突判定都
//     不能因去空白而返回另一份申请或报内容冲突；
//   - 完整学生编号不存在时退出码 1 并说明该学生不存在，不给其他学生
//     新增历史、不改文件；
//   - 学生存在而完整要求编号不存在时（即使去掉空白能找到要求）拒绝该
//     申请（退出码 1），但按原规则在申请人名下保存原要求编号、免修编号、
//     依据原文与拒绝原因，供 show/check 核对查看，不获得学分；
//   - 编号为空字符串仍拒绝，不创建申请；
//   - 新编号申请同一项已有有效免修的要求，仍保留一条被拒绝的新申请，
//     不产生第二份有效免修；已拒绝的申请重复提交不重新生效。

// waiverWhitespaceCLITwoStudents 构造 "s1" 与 " s1 " 两名学生：各有一项
// r1 要求，分别指向 4 学分课程 c1 与 3 学分课程 c2，两人均无修读无免修。
func waiverWhitespaceCLITwoStudents() *diskRecord {
	return &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
			{ID: "r1", Student: " s1 ", Course: "c2"},
		},
	}
}

// TestCLIWaiverWhitespaceStudentIDHitsExactOwner 给 " s1 " 提交申请：
// 只命中本人的 r1、获得本人 3 学分；提示与 show/check 都保留完整编号，
// "s1" 的学分、要求与历史保持原样。
func TestCLIWaiverWhitespaceStudentIDHitsExactOwner(t *testing.T) {
	file, _ := writeDiskRecord(t, waiverWhitespaceCLITwoStudents())

	out, errText, code := runCLI(t, file, "waiver", " s1 ", "r1", "w1", "学科竞赛获奖")
	if code != 0 {
		t.Fatalf("给 \" s1 \" 的申请应成功，code=%d out=%q err=%q", code, out, errText)
	}
	// 提示使用申请实际的完整学生编号、要求编号与免修编号。
	for _, want := range []string{"免修 w1 有效", "学生  s1 ", "要求 r1", "学科竞赛获奖", "获得课程学分"} {
		if !strings.Contains(out, want) {
			t.Fatalf("成功提示应包含 %q，out=%q", want, out)
		}
	}

	// " s1 " 本人：3 学分，来源是本人 w1；课程是本人的 3 学分课程。
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != 0 {
		t.Fatalf("check \" s1 \" 应成功，code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"总学分：3",
		"要求 r1（课程 c2《线性代数》，3 学分）：已满足",
		"来源为有效免修 w1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("\" s1 \" 应凭本人 w1 获得 3 学分，out=%q（缺 %q）", out, want)
		}
	}

	// "s1" 保持原样：0 学分、r1 未满足、无任何免修记录。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") ||
		strings.Contains(out, "来源为有效免修") || strings.Contains(out, "免修 w1") {
		t.Fatalf("\"s1\" 应仍为 0 学分、r1 未满足且无免修历史，code=%d out=%q", code, out)
	}

	// show 中历史只挂在 " s1 " 名下，并保留完整编号与依据原文。
	show, _, code := runCLI(t, file, "show", " s1 ")
	if code != 0 || !strings.Contains(show, `免修 w1：要求 r1，依据 "学科竞赛获奖"，状态：有效`) {
		t.Fatalf("\" s1 \" 的 show 应保留申请原文，code=%d show=%q", code, show)
	}
	showS1, _, _ := runCLI(t, file, "show", "s1")
	if strings.Contains(showS1, "免修 w1") {
		t.Fatalf("\"s1\" 的 show 不应出现他人申请，show=%q", showS1)
	}

	// 文件确实因新建申请而更新：另一名学生的原始要求与学生记录仍在文件
	// 中、编号未被修剪或改写，新申请挂在完整编号的申请人名下。
	assertFileContains(t, file, `"student": " s1 "`)
	assertFileContains(t, file, `"id": " s1 "`)
	assertFileContains(t, file, `"student": "s1"`)
}

// assertFileContains 断言记录文件包含某段原文。
func assertFileContains(t *testing.T, file, want string) {
	t.Helper()
	got := mustReadRecord(t, file)
	if !strings.Contains(got, want) {
		t.Fatalf("记录文件应包含 %q，实际=%s", want, got)
	}
}

// TestCLIWaiverWhitespaceReqIDIndependent 同一学生名下 "r1"（4 学分）与
// " r1 "（3 学分）是两项要求：给 " r1 " 的申请只满足后者，r1 保持未满足。
func TestCLIWaiverWhitespaceReqIDIndependent(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
			{ID: " r1 ", Student: "s1", Course: "c2"},
		},
	}
	file, _ := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "waiver", "s1", " r1 ", "w1", "外校同层次课程")
	if code != 0 {
		t.Fatalf("给 \" r1 \" 的申请应成功，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(out, "要求  r1 ") {
		t.Fatalf("提示应保留完整要求编号 \" r1 \"，out=%q", out)
	}

	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：3") {
		t.Fatalf("只应计 \" r1 \" 对应课程的 3 学分，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "要求  r1 （课程 c2《线性代数》，3 学分）：已满足") ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("\" r1 \" 应由 w1 满足并计 3 学分，out=%q", out)
	}
	if !strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：未满足") {
		t.Fatalf("不带空白的 r1 应保持未满足，out=%q", out)
	}
	if !strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("未满足要求应仍为 [r1]，out=%q", out)
	}
}

// TestCLIWaiverWhitespaceWaiverIDIndependent "w1" 与 " w1 " 是两份不同
// 申请：原内容重复提交返回带空格的那份（退出码 0），换内容冲突时只点名
// 带空格申请及其依据，绝不引用另一份申请。
func TestCLIWaiverWhitespaceWaiverIDIndependent(t *testing.T) {
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
			{ID: "w1", Student: "s1", Req: "r1", Basis: "依据一", Status: "approved"},
			{ID: " w1 ", Student: "s1", Req: "r2", Basis: "依据二", Status: "approved"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	// " w1 " 原内容重复提交：幂等返回带空格的原申请。
	out, errText, code := runCLI(t, file, "waiver", "s1", "r2", " w1 ", "依据二")
	if code != 0 || !strings.Contains(out, "免修  w1  已提交过且内容一致") {
		t.Fatalf("\" w1 \" 原内容重复提交应幂等返回原申请，code=%d out=%q err=%q",
			code, out, errText)
	}
	assertFileByteIdentical(t, file, raw, "幂等重复提交：")

	// " w1 " 换依据：退出码 1，冲突只点名 " w1 " 与依据二，不能出现 "w1"
	// 的依据一；文件保持原样。
	out, errText, code = runCLI(t, file, "waiver", "s1", "r2", " w1 ", "换成别的依据")
	if code != exitRejected {
		t.Fatalf("同编号换内容应业务拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(errText, " w1 ") || !strings.Contains(errText, "依据二") {
		t.Fatalf("冲突错误应点名 \" w1 \" 及其原依据，err=%q", errText)
	}
	if strings.Contains(errText, "依据一") {
		t.Fatalf("冲突错误不能引用另一份申请 \"w1\" 的依据一，err=%q", errText)
	}
	assertFileByteIdentical(t, file, raw, "换内容冲突：")

	// 两份申请都仍有效，学分各计一份（共 7）。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：7") ||
		strings.Count(out, "来源为有效免修") != 2 {
		t.Fatalf("两份独立申请应各自满足一项要求、共 7 学分，code=%d out=%q", code, out)
	}
}

// TestCLIWaiverWhitespaceStudentNotFoundHardReject 完整学生编号不存在：
// 退出码 1、说明学生不存在，不给文件里任何学生新增免修历史，文件逐字节
// 保持原样。普通空格、制表符、全角空格一样不能被忽略。
func TestCLIWaiverWhitespaceStudentNotFoundHardReject(t *testing.T) {
	cases := []struct {
		label   string
		student string
	}{
		{"前导普通空格", " s1"},
		{"尾随普通空格", "s1 "},
		{"制表符", "\ts1\t"},
		{"全角空格", "　s1　"},
		{"纯空白编号", "  "},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			file, raw := writeDiskRecord(t, waiverWhitespaceCLITwoStudents())

			out, errText, code := runCLI(t, file, "waiver", tc.student, "r1", "w1", "学科竞赛获奖")
			if code != exitRejected {
				t.Fatalf("%s：应业务拒绝（退出码 1），code=%d out=%q err=%q",
					tc.label, code, out, errText)
			}
			if !strings.Contains(errText, "学生") || !strings.Contains(errText, "不存在") {
				t.Fatalf("%s：错误应说明学生不存在，err=%q", tc.label, errText)
			}
			if strings.Contains(out, "有效") || strings.Contains(out, "已记入") {
				t.Fatalf("%s：被拒绝时不应输出申请成功或已入历史的提示，out=%q", tc.label, out)
			}
			assertFileByteIdentical(t, file, raw, tc.label+"：")

			// 两名已有学生的历史都必须保持为空。
			for _, sid := range []string{"s1", " s1 "} {
				show, _, _ := runCLI(t, file, "show", sid)
				if strings.Contains(show, "免修 w1") {
					t.Fatalf("%s：不应给 %s 新增免修历史", tc.label, sid)
				}
			}
		})
	}
}

// TestCLIWaiverWhitespaceReqNotFoundSavesRawIDsUnderApplicant 学生存在而
// 完整要求编号不存在（即使去掉空白能碰上本人的另一项要求）：退出码 1，
// 但申请按原规则保存在申请人名下——原要求编号、免修编号、依据原文与
// 拒绝原因都可在 show/check 中查到，不获得学分；重复提交返回原拒绝结果。
func TestCLIWaiverWhitespaceReqNotFoundSavesRawIDsUnderApplicant(t *testing.T) {
	cases := []struct {
		label   string
		student string
		req     string
	}{
		{"普通空格要求编号", "s1", " r1 "},
		{"制表符要求编号", "s1", "\tr1\t"},
		{"全角空格要求编号", "s1", "　r1　"},
		{"带空白学生与要求", " s1 ", " r1 "},
		{"完全不存在的要求编号", "s1", "rX"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			file, _ := writeDiskRecord(t, waiverWhitespaceCLITwoStudents())

			out, errText, code := runCLI(t, file, "waiver", tc.student, tc.req, " w9 ", "实际依据文字")
			if code != exitRejected {
				t.Fatalf("%s：目标要求不存在应业务拒绝，code=%d out=%q err=%q",
					tc.label, code, out, errText)
			}
			// 提示保留完整免修编号，并说明已记入申请人历史。
			if !strings.Contains(out, "免修申请  w9  已拒绝") ||
				!strings.Contains(out, "已记入学生") {
				t.Fatalf("%s：应说明申请已拒绝并记入历史，out=%q", tc.label, out)
			}

			// 申请人核对：不学分别人的学分（s1=0、 s1 =0），被拒绝申请
			// 按原要求编号列出。
			check, _, code := runCLI(t, file, "check", tc.student)
			if code != 0 || !strings.Contains(check, "总学分：0") {
				t.Fatalf("%s：申请人不应获得学分，code=%d check=%q", tc.label, code, check)
			}
			wantLine := "免修  w9 （要求 " + tc.req + "，依据 \"实际依据文字\"）"
			if !strings.Contains(check, wantLine) {
				t.Fatalf("%s：核对应按原要求编号列出拒绝记录 %q，check=%q",
					tc.label, wantLine, check)
			}

			// show 同样保留原要求编号、免修编号、依据与拒绝原因。
			show, _, code := runCLI(t, file, "show", tc.student)
			if code != 0 || !strings.Contains(show,
				"免修  w9 ：要求 "+tc.req+"，依据 \"实际依据文字\"，状态：已拒绝") {
				t.Fatalf("%s：show 应保留完整编号与依据原文，code=%d show=%q",
					tc.label, code, show)
			}

			// 原内容重复提交：返回原拒绝结果，退出码 0，不新增申请。
			out2, _, code := runCLI(t, file, "waiver", tc.student, tc.req, " w9 ", "实际依据文字")
			if code != 0 || !strings.Contains(out2, "免修  w9  已提交过且内容一致") {
				t.Fatalf("%s：重复提交应幂等返回原拒绝申请，code=%d out2=%q",
					tc.label, code, out2)
			}
			show, _, _ = runCLI(t, file, "show", tc.student)
			if strings.Count(show, "免修  w9 ") != 1 {
				t.Fatalf("%s：重复提交不应新增申请，show=%q", tc.label, show)
			}

			// 另一名学生名下不得出现任何申请。
			other := "s1"
			if tc.student == "s1" {
				other = " s1 "
			}
			showOther, _, _ := runCLI(t, file, "show", other)
			if strings.Contains(showOther, "w9") {
				t.Fatalf("%s：拒绝记录不应挂到另一名学生 %s 名下，show=%q",
					tc.label, other, showOther)
			}
		})
	}
}

// TestCLIWaiverWhitespaceEmptyIDRejected 学生、要求、免修编号为空字符串时
// 退出码 1 且不创建申请，文件逐字节保持原样。
func TestCLIWaiverWhitespaceEmptyIDRejected(t *testing.T) {
	file, raw := writeDiskRecord(t, waiverWhitespaceCLITwoStudents())
	cases := [][]string{
		{"", "r1", "w1", "依据"},
		{"s1", "", "w1", "依据"},
		{"s1", "r1", "", "依据"},
	}
	for _, args := range cases {
		out, errText, code := runCLI(t, file, append([]string{"waiver"}, args...)...)
		if code != exitRejected {
			t.Fatalf("空编号 %v 应业务拒绝，code=%d out=%q err=%q", args, code, out, errText)
		}
		if !strings.Contains(errText, "不能为空") {
			t.Fatalf("错误应说明编号为空，err=%q", errText)
		}
		assertFileByteIdentical(t, file, raw, "空编号拒绝：")
	}
	for _, sid := range []string{"s1", " s1 "} {
		show, _, _ := runCLI(t, file, "show", sid)
		if strings.Contains(show, "免修 w1") {
			t.Fatalf("空编号拒绝不得创建任何申请，show(%s)=%q", sid, show)
		}
	}
}

// TestCLIWaiverNewIDForAlreadyWaivedReqKeepsRejectedRecord 新免修编号申请
// 一项已有有效免修的要求：退出码 1，但保留一条独立的被拒绝新申请（不产生
// 第二份有效免修）；原有效免修仍是唯一学分来源，重复提交拒绝记录不重生。
func TestCLIWaiverNewIDForAlreadyWaivedReqKeepsRejectedRecord(t *testing.T) {
	d := waiverWhitespaceCLITwoStudents()
	d.Waivers = []diskWaiver{
		{ID: "w1", Student: " s1 ", Req: "r1", Basis: "原获奖材料", Status: "approved"},
	}
	file, _ := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "waiver", " s1 ", "r1", " w2 ", "新的外校修读证明")
	if code != exitRejected || !strings.Contains(out, "已拒绝") {
		t.Fatalf("已有有效免修时新编号申请应被拒绝并保留，code=%d out=%q", code, out)
	}

	check, _, code := runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(check, "总学分：3") {
		t.Fatalf("仍应以原有效免修计一次 3 学分，code=%d check=%q", code, check)
	}
	for _, want := range []string{
		"来源为有效免修 w1",
		`免修  w2 （要求 r1，依据 "新的外校修读证明"）：该要求已有有效免修 w1`,
	} {
		if !strings.Contains(check, want) {
			t.Fatalf("核对应保留原来源并列出被拒绝的新申请，check=%q（缺 %q）", check, want)
		}
	}

	// 被拒绝的新申请原样重复提交：仍退出码 0 幂等返回，不重新生效、不新增。
	out, _, code = runCLI(t, file, "waiver", " s1 ", "r1", " w2 ", "新的外校修读证明")
	if code != 0 || !strings.Contains(out, "状态：已拒绝") {
		t.Fatalf("被拒绝申请重复提交应幂等返回原拒绝结果，code=%d out=%q", code, out)
	}
	show, _, _ := runCLI(t, file, "show", " s1 ")
	if strings.Count(show, "免修  w2 ") != 1 {
		t.Fatalf("被拒绝新申请应只保留一条，show=%q", show)
	}
	if strings.Count(show, "免修 w1：") != 1 {
		t.Fatalf("原有效免修应保持唯一，show=%q", show)
	}
}
