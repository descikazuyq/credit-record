package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“course 按用户给出的完整课程编号区分课程”。
// 每次调用都重新打开记录文件，因此下列检查同时覆盖落盘后再加载的结果。
// 课程编号必须与学生、要求、修读、免修编号采用同一套口径：前后普通空格、
// 制表符、全角空格（U+3000）都是编号内容，绝不修剪。
//   - 首次分别登记 "c1" 与 " c1 " 保存两门独立课程，各自保留完整编号、
//     名称与正整数学分，初始开放，课程列表分别显示；
//   - 文件中只有 "c1" 时提交 " c1 " 必须新建后者，不能借用前者的资料
//     或停开状态；再次提交某个完整编号只判断对应课程：同内容返回原记录，
//     内容不同按现有规则原地更新，不新增副本、不改动另一门课程；
//   - 学分锁定跟随实际命中的课程：要求引用 " c1 " 时只有它不能改学分，
//     未被引用的 "c1" 不受影响，反过来也一样；同时改名改学分整次拒绝
//     （退出码 1），原名称、学分、开放状态与记录文件全部保留；保持原
//     学分只改名允许，已有要求、修读、免修继续关联，核对结论不变；
//   - 空字符串与全空白编号拒绝，stdout 不提示成功，记录文件逐字节不变；
//   - 已有文件中的带空白编号（含停开课程）按原编号得到同样处理。

// courseWhitespaceCLIRecord 构造两门同号不同空白课程并存的合法记录：
// "c1"（高等数学 4 学分，开放）与 " c1 "（大学物理 3 学分，开放）。
func courseWhitespaceCLIRecord() *diskRecord {
	return &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: true},
		},
	}
}

// TestCLICourseWhitespaceTwoDistinctRegistrations 主场景：从空记录首次
// 分别登记 "c1" 与 " c1 "，保存两门独立课程，编号、名称、学分各自保留，
// 初始开放，课程列表分别显示；落盘重开后仍然如此。
func TestCLICourseWhitespaceTwoDistinctRegistrations(t *testing.T) {
	file := tempRecordFile(t)

	out, errText, code := runCLI(t, file, "course", "c1", "高等数学", "4")
	if code != 0 || !strings.Contains(out, "已登记课程 c1《高等数学》4 学分，状态：开放") {
		t.Fatalf("登记 c1 应成功，code=%d out=%q err=%q", code, out, errText)
	}
	out, errText, code = runCLI(t, file, "course", " c1 ", "大学物理", "3")
	if code != 0 || !strings.Contains(out, "已登记课程  c1 《大学物理》3 学分，状态：开放") {
		t.Fatalf("登记 \" c1 \" 应作为另一门课程新建并保留完整编号，code=%d out=%q err=%q",
			code, out, errText)
	}

	out, _, code = runCLI(t, file, "list-courses")
	if code != 0 {
		t.Fatalf("列出课程失败 code=%d out=%q", code, out)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("课程列表应分别显示两门课程，实际：%q", out)
	}
	if !strings.Contains(out, "课程 c1《高等数学》4 学分，状态：开放") ||
		!strings.Contains(out, "课程  c1 《大学物理》3 学分，状态：开放") {
		t.Fatalf("两门课程应各自按完整编号、名称、学分显示，out=%q", out)
	}

	// 其他空白写法（制表符、全角空格）同样是独立课程。
	for _, st := range [][]string{
		{"course", "\tc1\t", "制表课程", "1"},
		{"course", "　c1　", "全角课程", "2"},
	} {
		if out, errText, code := runCLI(t, file, st...); code != 0 ||
			!strings.Contains(out, "已登记课程") {
			t.Fatalf("步骤 %v 应作为独立课程新建，code=%d out=%q err=%q", st, code, out, errText)
		}
	}
	out, _, _ = runCLI(t, file, "list-courses")
	if strings.Count(out, "\n") != 4 ||
		!strings.Contains(out, "课程 \tc1\t《制表课程》1 学分，状态：开放") ||
		!strings.Contains(out, "课程 　c1　《全角课程》2 学分，状态：开放") {
		t.Fatalf("四种编号写法应保存四门课程并分别显示，out=%q", out)
	}
}

// TestCLICourseWhitespaceNoBorrowWhenOnlyTrimmedExists 文件中只有 "c1"
// （且已停开）时，提交 " c1 " 必须新建后者：新课程初始开放，不借用
// "c1" 的名称、学分或停开状态；"c1" 原资料保持不变。
func TestCLICourseWhitespaceNoBorrowWhenOnlyTrimmedExists(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: false},
		},
		// 带空白的学生编号直接写入文件（student 命令沿用修剪口径，这里
		// 只验证 course 对带空白编号的处理）。
		Students: []diskStudent{{ID: " s1 "}, {ID: "s1"}},
	}
	file, _ := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "course", " c1 ", "大学物理", "3")
	if code != 0 || !strings.Contains(out, "已登记课程  c1 《大学物理》3 学分，状态：开放") {
		t.Fatalf("文件中只有 c1 时提交 \" c1 \" 应新建且初始开放，code=%d out=%q err=%q",
			code, out, errText)
	}

	out, _, code = runCLI(t, file, "list-courses")
	if code != 0 || strings.Count(out, "\n") != 2 {
		t.Fatalf("应显示两门课程，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "课程 c1《高等数学》4 学分，状态：停开") ||
		!strings.Contains(out, "课程  c1 《大学物理》3 学分，状态：开放") {
		t.Fatalf("新课程不应借用 c1 的资料或停开状态，out=%q", out)
	}

	// 新课程开放、可建要求与选课；c1 仍停开（停开课程仍可建立要求）。
	if _, errText, code := runCLI(t, file, "req", " s1 ", " r1 ", " c1 "); code != 0 {
		t.Fatalf("应能对新建的 \" c1 \" 建立要求，code=%d err=%q", code, errText)
	}
	if _, errText, code := runCLI(t, file, "enroll", " s1 ", " r1 ", "2024春", "e1"); code != 0 {
		t.Fatalf("开放的 \" c1 \" 应能新增修读，code=%d err=%q", code, errText)
	}
	if _, _, code := runCLI(t, file, "req", "s1", "rX", "c1"); code != 0 {
		t.Fatal("为停开的 c1 建立要求应仍允许")
	}
	if _, errText, code := runCLI(t, file, "enroll", "s1", "rX", "2024春", "eX"); code != 1 {
		t.Fatalf("停开的 c1 不应因新课程开放而能选课，code=%d err=%q", code, errText)
	}
}

// TestCLICourseWhitespaceExactReregister 再次提交某个完整编号时只判断
// 对应课程：同内容返回原记录（不写文件）；不同内容在未被引用时原地更新
// 对应课程，不新增副本、不改动另一门课程。
func TestCLICourseWhitespaceExactReregister(t *testing.T) {
	file, _ := writeDiskRecord(t, courseWhitespaceCLIRecord())

	// 相同完整编号 + 相同内容：返回原记录，文件不变。
	before := mustReadRecord(t, file)
	out, errText, code := runCLI(t, file, "course", " c1 ", "大学物理", "3")
	if code != 0 || !strings.Contains(out, "已存在且内容一致，返回原记录") {
		t.Fatalf("相同完整编号同内容应幂等返回，code=%d out=%q err=%q", code, out, errText)
	}
	assertRecordUnchanged(t, file, before, "幂等重复：")

	// 用 " c1 " 提交新名称新学分：只更新 " c1 "，c1 保持原样。
	out, errText, code = runCLI(t, file, "course", " c1 ", "大学物理（上）", "5")
	if code != 0 || !strings.Contains(out, "已更新") {
		t.Fatalf("未被引用的 \" c1 \" 应允许原地更新，code=%d out=%q err=%q", code, out, errText)
	}
	assertSingleCourseLine(t, file, "课程  c1 《大学物理（上）》5 学分，状态：开放", " c1 ")
	out, _, _ = runCLI(t, file, "list-courses")
	if !strings.Contains(out, "课程 c1《高等数学》4 学分，状态：开放") {
		t.Fatalf("更新 \" c1 \" 不应改动另一门课程 c1，out=%q", out)
	}

	// 用 "c1" 更新：只动 c1。
	if out, errText, code := runCLI(t, file, "course", "c1", "高等数学（下）", "6"); code != 0 {
		t.Fatalf("c1 应独立更新，code=%d out=%q err=%q", code, out, errText)
	}
	out, _, _ = runCLI(t, file, "list-courses")
	if !strings.Contains(out, "课程 c1《高等数学（下）》6 学分，状态：开放") ||
		!strings.Contains(out, "课程  c1 《大学物理（上）》5 学分，状态：开放") {
		t.Fatalf("两门课程应各自保留自己的更新，out=%q", out)
	}

	// 去空白提交即便名称学分与某门课一致，也不能返回那门课：
	// 文件里没有 "\tc1\t"，应新建第三门。
	out, _, code = runCLI(t, file, "course", "\tc1\t", "高等数学（下）", "6")
	if code != 0 || !strings.Contains(out, "已登记课程 \tc1\t《高等数学（下）》6 学分，状态：开放") {
		t.Fatalf("编号不同即使内容一致也应新建，code=%d out=%q", code, out)
	}
	out, _, _ = runCLI(t, file, "list-courses")
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("应保存三门课程，out=%q", out)
	}
}

// TestCLICourseWhitespaceReferenceLockFollowsExactCourse 学分锁定跟随
// 实际命中的课程：引用 " c1 " 只锁定它（改名+改学分整次拒绝、文件不
// 变、stdout 无成功提示；保持原学分只改名允许），未被引用的 "c1" 仍可
// 更新；随后反向验证引用 "c1" 不影响 " c1 "。
func TestCLICourseWhitespaceReferenceLockFollowsExactCourse(t *testing.T) {
	d := courseWhitespaceCLIRecord()
	d.Students = []diskStudent{{ID: "s1"}}
	d.Requirements = []diskReq{
		{ID: "r1", Student: "s1", Course: " c1 "},
	}
	file, raw := writeDiskRecord(t, d)

	// " c1 " 被引用：同时改名改学分整次拒绝。
	out, errText, code := runCLI(t, file, "course", " c1 ", "大学物理（下）", "9")
	if code != exitRejected {
		t.Fatalf("被引用的 \" c1 \" 改学分应退出码 1，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(errText, "已被课程要求引用") || !strings.Contains(errText, "原学分 3") {
		t.Fatalf("拒绝说明应指出被引用与原学分 3，err=%q", errText)
	}
	if strings.Contains(out, "已登记课程") || strings.Contains(out, "已更新") ||
		strings.Contains(out, "返回原记录") {
		t.Fatalf("被拒绝时 stdout 不应出现登记或更新成功提示，out=%q", out)
	}
	assertFileByteIdentical(t, file, raw, "被引用课程整次更新：")
	assertSingleCourseLine(t, file, "课程  c1 《大学物理》3 学分，状态：开放", " c1 ")

	// 未被引用的 "c1" 不受影响，仍可改名改学分。
	if out, errText, code := runCLI(t, file, "course", "c1", "高等数学（下）", "6"); code != 0 {
		t.Fatalf("仅 \" c1 \" 被引用不应阻止 c1 更新，code=%d out=%q err=%q", code, out, errText)
	}
	assertSingleCourseLine(t, file, "课程 c1《高等数学（下）》6 学分，状态：开放", "c1")

	// " c1 " 保持原学分只改名：允许；已有要求继续关联，核对按新课程名、
	// 原学分显示，未满足、总学分 0。
	if out, errText, code := runCLI(t, file, "course", " c1 ", "大学物理（实验班）", "3"); code != 0 {
		t.Fatalf("被引用课程保持原学分应允许改名，code=%d out=%q err=%q", code, out, errText)
	}
	assertSingleCourseLine(t, file, "课程  c1 《大学物理（实验班）》3 学分，状态：开放", " c1 ")
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 || !strings.Contains(out, "要求 r1 -> 课程  c1 《大学物理（实验班）》3 学分") {
		t.Fatalf("已有要求应继续关联 \" c1 \" 并显示新名称，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "要求 r1（课程  c1 《大学物理（实验班）》，3 学分）：未满足") {
		t.Fatalf("改名后要求仍未满足、学分为 3、总学分 0，code=%d out=%q", code, out)
	}

	// 反向场景（另一份记录）："c1" 被引用时，未被任何要求引用的
	// " c1 " 仍可改名改学分；而 "c1" 改学分被拒绝。
	dRev := courseWhitespaceCLIRecord()
	dRev.Students = []diskStudent{{ID: "s1"}}
	dRev.Requirements = []diskReq{
		{ID: "r1", Student: "s1", Course: "c1"},
	}
	fileRev, _ := writeDiskRecord(t, dRev)
	if _, errText, code := runCLI(t, fileRev, "course", "c1", "再改名称", "7"); code != 1 ||
		!strings.Contains(errText, "已被课程要求引用") {
		t.Fatalf("被引用的 c1 改学分应拒绝，code=%d err=%q", code, errText)
	}
	if out, errText, code := runCLI(t, fileRev, "course", " c1 ", "大学物理（重修班）", "8"); code != 0 {
		t.Fatalf("未被引用的 \" c1 \" 应允许更新，code=%d out=%q err=%q", code, out, errText)
	}
	assertSingleCourseLine(t, fileRev, "课程  c1 《大学物理（重修班）》8 学分，状态：开放", " c1 ")
	assertSingleCourseLine(t, fileRev, "课程 c1《高等数学》4 学分，状态：开放", "c1")
}

// TestCLICourseWhitespaceReferencedRenameKeepsSatisfaction 被引用的带空白
// 课程保持原学分改名后，已有要求、通过修读与有效免修继续关联原课程，
// 核对的满足情况、学分来源与总学分不变。
func TestCLICourseWhitespaceReferencedRenameKeepsSatisfaction(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"student", "s2"},
		{"course", " c1 ", "大学物理", "3"},
		{"req", "s1", "r1", " c1 "},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
		{"req", "s2", "r1", " c1 "},
		{"waiver", "s2", "r1", "w1", "外校同层次课程"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 保持学分 3 只改名。
	if out, errText, code := runCLI(t, file, "course", " c1 ", "大学物理（强化班）", "3"); code != 0 {
		t.Fatalf("被引用课程相同学分只改名应成功，code=%d out=%q err=%q", code, out, errText)
	}
	assertSingleCourseLine(t, file, "课程  c1 《大学物理（强化班）》3 学分，状态：开放", " c1 ")

	// s1 仍凭通过修读 e1 满足、总学分 3、来源不变，只显示新名称。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：3") ||
		!strings.Contains(out, "要求 r1（课程  c1 《大学物理（强化班）》，3 学分）：已满足") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("s1 改名后应仍凭 e1 满足、总学分 3，code=%d out=%q", code, out)
	}
	// s2 仍凭有效免修 w1 满足、总学分 3、来源不变。
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：3") ||
		!strings.Contains(out, "来源为有效免修 w1") ||
		!strings.Contains(out, "大学物理（强化班）") {
		t.Fatalf("s2 改名后应仍凭 w1 满足、总学分 3，code=%d out=%q", code, out)
	}
}

// TestCLICourseWhitespaceClosedCourseStaysClosed 已有文件中的停开带空白
// 课程按原编号维护：同内容重新登记仍停开；未被引用时改名改学分后仍停开；
// 被引用后整次拒绝保持停开。
func TestCLICourseWhitespaceClosedCourseStaysClosed(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: false},
		},
	}
	file, _ := writeDiskRecord(t, d)

	// 同内容重新登记：返回原记录，状态保持停开。
	if out, errText, code := runCLI(t, file, "course", " c1 ", "大学物理", "3"); code != 0 ||
		!strings.Contains(out, "已存在且内容一致，返回原记录（状态保持：停开）") {
		t.Fatalf("同内容重新登记停开课程应返回原记录且仍停开，code=%d out=%q err=%q",
			code, out, errText)
	}
	// 未被引用时改名改学分：更新后仍停开。
	if out, errText, code := runCLI(t, file, "course", " c1 ", "大学物理（下）", "6"); code != 0 {
		t.Fatalf("未被引用的停开课程应允许更新，code=%d out=%q err=%q", code, out, errText)
	}
	assertSingleCourseLine(t, file, "课程  c1 《大学物理（下）》6 学分，状态：停开", " c1 ")

	// 被引用后改名改学分：整次拒绝，停开与原资料保留。
	if _, _, code := runCLI(t, file, "student", "s1"); code != 0 {
		t.Fatal("登记学生失败")
	}
	if _, _, code := runCLI(t, file, "req", "s1", "r1", " c1 "); code != 0 {
		t.Fatal("建立要求失败")
	}
	before := mustReadRecord(t, file)
	if out, errText, code := runCLI(t, file, "course", " c1 ", "另一个名称", "7"); code != 1 {
		t.Fatalf("被引用后改学分应拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	assertRecordUnchanged(t, file, before, "停开课程被引用后的整次更新：")
	assertSingleCourseLine(t, file, "课程  c1 《大学物理（下）》6 学分，状态：停开", " c1 ")

	// 保持学分只改名：允许，仍停开。
	if out, errText, code := runCLI(t, file, "course", " c1 ", "大学物理（重修班）", "6"); code != 0 {
		t.Fatalf("保持学分只改名应成功，code=%d out=%q err=%q", code, out, errText)
	}
	assertSingleCourseLine(t, file, "课程  c1 《大学物理（重修班）》6 学分，状态：停开", " c1 ")
}

// TestCLICourseWhitespaceBlankIDRejected 空字符串与全部由空白字符组成
// （普通空格、制表符、全角空格，含混用）的课程编号一律业务拒绝：退出码 1，
// stderr 说明具体原因，stdout 不出现登记或更新成功提示，记录文件逐字节
// 不变。含实际文字的编号仍原样登记。
func TestCLICourseWhitespaceBlankIDRejected(t *testing.T) {
	d := courseWhitespaceCLIRecord()
	d.Students = []diskStudent{{ID: "s1"}}
	file, raw := writeDiskRecord(t, d)

	cases := []struct {
		label   string
		id      string
		wantErr []string
	}{
		{"空字符串", "", []string{"不能为空"}},
		{"普通空格", " ", []string{"空白字符"}},
		{"多个空格", "   ", []string{"空白字符"}},
		{"制表符", "\t", []string{"空白字符"}},
		{"全角空格", "　", []string{"空白字符"}},
		{"混合空白", " \t　", []string{"空白字符"}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			out, errText, code := runCLI(t, file, "course", tc.id, "课程名", "4")
			if code != exitRejected {
				t.Fatalf("%s：应退出码 1，code=%d out=%q err=%q", tc.label, code, out, errText)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(errText, want) {
					t.Fatalf("%s：stderr 应包含 %q，err=%q", tc.label, want, errText)
				}
			}
			if strings.Contains(out, "已登记课程") || strings.Contains(out, "已更新") ||
				strings.Contains(out, "返回原记录") {
				t.Fatalf("%s：被拒绝时 stdout 不应提示成功，out=%q", tc.label, out)
			}
			assertFileByteIdentical(t, file, raw, tc.label+"：")
		})
	}

	// 含实际文字的编号原样登记并参与重复判断。
	out, _, code := runCLI(t, file, "course", "  c9  ", "前沿讲座", "2")
	if code != 0 || !strings.Contains(out, "已登记课程   c9  《前沿讲座》2 学分，状态：开放") {
		t.Fatalf("含实际文字的编号应原样登记，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "course", "  c9  ", "前沿讲座", "2")
	if code != 0 || !strings.Contains(out, "已存在且内容一致，返回原记录") {
		t.Fatalf("相同完整编号应幂等返回，code=%d out=%q", code, out)
	}
	// 去空白后的 c9 不应命中 "  c9  "：学生存在、课程按完整编号找不到，
	// 明确报告课程不存在。
	if _, errText, code := runCLI(t, file, "req", "s1", "r1", "c9"); code != 1 ||
		!strings.Contains(errText, "课程") || !strings.Contains(errText, "不存在") {
		t.Fatalf("去空白后的 c9 不应命中 \"  c9  \"，code=%d err=%q", code, errText)
	}
}
