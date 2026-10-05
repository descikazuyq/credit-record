package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“req 按用户给出的完整学生编号、要求编号与
// 课程编号确定要求归属”。记录文件可以合法保存带前后空白（普通空格、
// 制表符、全角空格）的编号，show/check/enroll 能把它们区分，登记要求
// 也必须如此：
//   - 为 " s1 " 登记指向 " c1 " 的要求 " r1 " 只落在这名学生名下，
//     三个编号原文都保留；show 显示完整要求编号和实际指向课程的名称、
//     学分；其他学生的要求与学分保持原样；
//   - 同一学生名下 "r1" 与 " r1 " 是两项要求，但仍须指向不同课程；
//     同一学生不能换编号为同一门课程再建要求；完整三元组相同返回原
//     记录；同一完整要求编号改指另一课程整次拒绝、保留原指向；
//   - 完整学生编号或完整课程编号找不到时按业务拒绝（退出码 1），
//     stderr 指出是哪一个对象不存在，stdout 不出现登记成功或返回原
//     要求的提示；即使去掉空白能碰上另一条记录也不借用，更不自动
//     创建学生或课程，记录文件逐字节保持原样；
//   - 空字符串编号仍拒绝，要求编号全部由空白字符组成时拒绝；
//   - 课程停开不改变建立要求的规则，新要求本身不带来学分。

// reqWhitespaceCLIRecord 构造合法记录：学生 "s1" 与 " s1 "、课程
// "c1"（高等数学 4 学分）与 " c1 "（大学物理 3 学分）；"s1" 名下
// 已有要求 r1 指向 "c1"，" s1 " 名下尚无要求。
func reqWhitespaceCLIRecord() *diskRecord {
	return &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s1"}, {ID: " s1 "}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
		},
	}
}

// TestCLIReqWhitespaceAllIDsPreserved 题目主场景：为 " s1 " 登记指向
// " c1 " 的要求 " r1 "，只在该学生名下保存一份，三个编号原文保留；
// show 显示完整要求编号与实际指向课程的名称、学分；"s1" 的要求与
// 学分保持原样。
func TestCLIReqWhitespaceAllIDsPreserved(t *testing.T) {
	file, _ := writeDiskRecord(t, reqWhitespaceCLIRecord())

	out, errText, code := runCLI(t, file, "req", " s1 ", " r1 ", " c1 ")
	if code != 0 {
		t.Fatalf("登记应成功，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(out, "已为学生  s1  登记要求  r1 ，指向课程  c1 ") {
		t.Fatalf("成功提示应保留三个编号原文，out=%q", out)
	}

	// show " s1 "：完整要求编号 + 实际指向课程 " c1 " 的名称、学分。
	out, _, code = runCLI(t, file, "show", " s1 ")
	if code != 0 {
		t.Fatalf("show \" s1 \" 应成功，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "要求  r1  -> 课程  c1 《大学物理》3 学分") {
		t.Fatalf("应显示完整要求编号与实际课程名称、学分，out=%q", out)
	}
	if strings.Contains(out, "高等数学") {
		t.Fatalf("\" s1 \" 的要求不应指向 \"s1\" 的课程 c1，out=%q", out)
	}

	// "s1" 的要求与课程指向保持原样。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 || !strings.Contains(out, "要求 r1 -> 课程 c1《高等数学》4 学分") {
		t.Fatalf("\"s1\" 的要求应保持原样，code=%d out=%q", code, out)
	}
	if strings.Contains(out, "要求  r1  -> 课程  c1 ") || strings.Contains(out, "大学物理") {
		t.Fatalf("新要求不应出现在 \"s1\" 名下，out=%q", out)
	}

	// 两名学生各自核对：新要求未满足、都没有学分，未满足要求编号按
	// 各自原文列出。
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[ r1 ]") {
		t.Fatalf("\" s1 \" 应为 0 学分且 \" r1 \" 未满足，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("\"s1\" 应仍为 0 学分且 r1 未满足，code=%d out=%q", code, out)
	}
}

// TestCLIReqWhitespaceDistinctReqIDDifferentCourses 同一学生名下
// "r1" 与 " r1 " 是两项要求，但仍须指向不同课程：换课程可并存；
// 同一完整编号改指课程整次拒绝；同一课程换编号再建也拒绝。
func TestCLIReqWhitespaceDistinctReqIDDifferentCourses(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "大学物理", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
		},
	}
	file, _ := writeDiskRecord(t, d)

	// " r1 " 指向另一门课程 c2：作为独立要求新建。
	out, errText, code := runCLI(t, file, "req", "s1", " r1 ", "c2")
	if code != 0 || !strings.Contains(out, "已为学生 s1 登记要求  r1 ，指向课程 c2") {
		t.Fatalf("同号不同空白的要求指向不同课程应允许，code=%d out=%q err=%q",
			code, out, errText)
	}
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "要求 r1 -> 课程 c1《高等数学》4 学分") ||
		!strings.Contains(show, "要求  r1  -> 课程 c2《大学物理》3 学分") {
		t.Fatalf("两项要求应各自保留并显示对应课程，show=%q", show)
	}

	before := mustReadRecord(t, file)

	// 同一完整要求编号 " r1 " 改指 c1：整次拒绝，保留原指向 c2。
	out, errText, code = runCLI(t, file, "req", "s1", " r1 ", "c1")
	if code != exitRejected || !strings.Contains(errText, "不能改指") {
		t.Fatalf("同一要求编号改指课程应拒绝，code=%d out=%q err=%q",
			code, out, errText)
	}
	if strings.Contains(out, "已为学生") || strings.Contains(out, "返回原记录") {
		t.Fatalf("被拒绝时 stdout 不应出现登记成功或返回原要求的提示，out=%q", out)
	}
	assertRecordUnchanged(t, file, before, "改指拒绝：")
	show, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "要求  r1  -> 课程 c2《大学物理》3 学分") ||
		strings.Contains(show, "要求  r1  -> 课程 c1") {
		t.Fatalf("拒绝改指后 \" r1 \" 应仍指向 c2，show=%q", show)
	}

	// 同一学生就同一课程换要求编号再建：拒绝。
	out, errText, code = runCLI(t, file, "req", "s1", "r2", "c1")
	if code != exitRejected || !strings.Contains(errText, "重复建立") {
		t.Fatalf("同一学生就同一课程换编号再建应拒绝，code=%d out=%q err=%q",
			code, out, errText)
	}
	if strings.Contains(out, "已为学生") {
		t.Fatalf("被拒绝时不应输出登记成功提示，out=%q", out)
	}
	assertRecordUnchanged(t, file, before, "同课程换编号拒绝：")
}

// TestCLIReqWhitespaceExactIdempotent 完整学生编号、要求编号、课程编号
// 均与已有要求相同时返回原记录，不新增要求、不改写文件；任一编号的
// 空白不同都不能算同一条记录。
func TestCLIReqWhitespaceExactIdempotent(t *testing.T) {
	file, _ := writeDiskRecord(t, reqWhitespaceCLIRecord())
	// 先建立 " s1 " 的 " r1 " -> " c1 "。
	if _, _, code := runCLI(t, file, "req", " s1 ", " r1 ", " c1 "); code != 0 {
		t.Fatal("首次登记应成功")
	}
	before := mustReadRecord(t, file)

	out, errText, code := runCLI(t, file, "req", " s1 ", " r1 ", " c1 ")
	if code != 0 || !strings.Contains(out, "已存在且内容一致，返回原记录") {
		t.Fatalf("完整三元组重复应返回原记录，code=%d out=%q err=%q", code, out, errText)
	}
	assertRecordUnchanged(t, file, before, "幂等重复：")

	// 学生编号去掉空白：命中的是另一名学生 "s1"——其名下没有 " r1 "，
	// 且课程若写 " c1 " 对 "s1" 是新课程；但三元组无论如何都不等于
	// 已有记录。这里要求 " r1 " 对 "s1" 尚不存在、课程 " c1 " 存在，
	// 同时 "s1" 尚未就 " c1 " 建立要求，所以它会作为另一名学生名下
	// 的独立新要求成功——这正是“编号按完整文字区分”的体现。
	out, _, code = runCLI(t, file, "req", "s1", " r1 ", " c1 ")
	if code != 0 || !strings.Contains(out, "已为学生 s1 登记要求  r1 ，指向课程  c1 ") {
		t.Fatalf("\"s1\" 与 \" s1 \" 是两名学生，同号要求应各自独立，code=%d out=%q",
			code, out)
	}
}

// TestCLIReqWhitespaceIDRejected 完整编号找不到时按业务拒绝：退出码 1，
// stderr 指出是学生还是课程不存在，stdout 不出现登记成功或返回原要求
// 的提示；不借用去空白后能碰上的记录、不自动创建；空字符串编号与只含
// 空白的要求编号同样拒绝；记录文件逐字节保持原样。
func TestCLIReqWhitespaceIDRejected(t *testing.T) {
	// 学生用例：文件里只有 "s1"。
	dStudent := &diskRecord{
		Version: 1,
		Courses: []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students: []diskStudent{
			{ID: "s1"},
		},
	}
	// 课程用例：有两名学生但只有课程 "c1"。
	dCourse := &diskRecord{
		Version: 1,
		Courses: []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students: []diskStudent{
			{ID: "s1"},
			{ID: " s1 "},
		},
	}

	cases := []struct {
		label   string
		d       *diskRecord
		args    []string
		wantErr []string
	}{
		// 学生编号带前后空白：应明确报告学生不存在。
		{"学生前导空格", dStudent, []string{"req", " s1", "r1", "c1"}, []string{"学生", "不存在"}},
		{"学生尾随空格", dStudent, []string{"req", "s1 ", "r1", "c1"}, []string{"学生", "不存在"}},
		{"学生制表符", dStudent, []string{"req", "\ts1\t", "r1", "c1"}, []string{"学生", "不存在"}},
		{"学生全角空格", dStudent, []string{"req", "　s1　", "r1", "c1"}, []string{"学生", "不存在"}},
		// 课程编号带前后空白：应明确报告课程不存在，不借用 "c1"。
		{"课程前导空格", dCourse, []string{"req", " s1 ", "r1", " c1"}, []string{"课程", "不存在"}},
		{"课程尾随空格", dCourse, []string{"req", " s1 ", "r1", "c1 "}, []string{"课程", "不存在"}},
		{"课程制表符", dCourse, []string{"req", " s1 ", "r1", "\tc1\t"}, []string{"课程", "不存在"}},
		{"课程全角空格", dCourse, []string{"req", " s1 ", "r1", "　c1　"}, []string{"课程", "不存在"}},
		// 空字符串编号仍拒绝。
		{"学生空字符串", dStudent, []string{"req", "", "r1", "c1"}, []string{"不能为空"}},
		{"要求空字符串", dStudent, []string{"req", "s1", "", "c1"}, []string{"不能为空"}},
		{"课程空字符串", dStudent, []string{"req", "s1", "r1", ""}, []string{"不能为空"}},
		// 要求编号全部由空白字符组成时拒绝。
		{"要求编号全空格", dStudent, []string{"req", "s1", "  ", "c1"}, []string{"要求编号"}},
		{"要求编号制表符", dStudent, []string{"req", "s1", "\t", "c1"}, []string{"要求编号"}},
		{"要求编号全角空格", dStudent, []string{"req", "s1", "　", "c1"}, []string{"要求编号"}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			file, raw := writeDiskRecord(t, tc.d)
			out, errText, code := runCLI(t, file, tc.args...)
			if code != exitRejected {
				t.Fatalf("%s：应业务拒绝（退出码 1），code=%d out=%q err=%q",
					tc.label, code, out, errText)
			}
			if strings.Contains(out, "已为学生") || strings.Contains(out, "返回原记录") {
				t.Fatalf("%s：被拒绝时 stdout 不应有登记成功或返回原要求的提示，out=%q",
					tc.label, out)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(errText, want) {
					t.Fatalf("%s：错误输出应包含 %q，err=%q", tc.label, want, errText)
				}
			}
			assertFileByteIdentical(t, file, raw, tc.label+"：")
		})
	}
}

// TestCLIReqWhitespaceExistingRecordsUntouched 已有带空白编号的合法
// 记录继续可读：被拒绝的登记不能改写、合并或重新解释其中的要求，冲突
// 前后记录文件逐字节一致。
func TestCLIReqWhitespaceExistingRecordsUntouched(t *testing.T) {
	file, _ := writeDiskRecord(t, reqWhitespaceCLIRecord())

	// 先在 " s1 " 名下建立 " r1 " -> " c1 "。
	if _, _, code := runCLI(t, file, "req", " s1 ", " r1 ", " c1 "); code != 0 {
		t.Fatal("首次登记应成功")
	}

	// 同一完整要求编号改指另一门课程：整次拒绝，文件逐字节保持原样。
	before := mustReadRecord(t, file)
	out, errText, code := runCLI(t, file, "req", " s1 ", " r1 ", "c1")
	if code != exitRejected || !strings.Contains(errText, "不能改指") {
		t.Fatalf("改指应拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	if strings.Contains(out, "已为学生") || strings.Contains(out, "返回原记录") {
		t.Fatalf("被拒绝时 stdout 不应有登记成功或返回原要求的提示，out=%q", out)
	}
	assertRecordUnchanged(t, file, before, "改指拒绝：")

	// 重新打开记录：带空白的编号原文（" s1 "、" r1 "、" c1 "）仍在，
	// 没有被修剪或合并；"s1" 的 r1 -> c1 也保持原样。
	out, _, code = runCLI(t, file, "show", " s1 ")
	if code != 0 || !strings.Contains(out, "要求  r1  -> 课程  c1 《大学物理》3 学分") {
		t.Fatalf("已有带空白编号的记录应原样保留，code=%d out=%q", code, out)
	}
	out, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(out, "要求 r1 -> 课程 c1《高等数学》4 学分") {
		t.Fatalf("\"s1\" 的原要求应保持，out=%q", out)
	}
}

// TestCLIReqWhitespaceClosedCourseAllowed 课程停开不改变建立要求的
// 规则：停开课程仍可登记要求，新要求不带来学分，show 仍显示课程名称
// 与学分；只是之后不能在该要求上新增修读。
func TestCLIReqWhitespaceClosedCourseAllowed(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: false},
		},
		Students: []diskStudent{{ID: " s1 "}},
	}
	file, _ := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "req", " s1 ", " r1 ", " c1 ")
	if code != 0 || !strings.Contains(out, "已为学生  s1  登记要求  r1 ，指向课程  c1 ") {
		t.Fatalf("停开课程仍应允许登记要求，code=%d out=%q err=%q", code, out, errText)
	}

	// 新要求本身不带来学分、未满足。
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[ r1 ]") {
		t.Fatalf("新要求应未满足且不计学分，code=%d out=%q", code, out)
	}
	// show 仍按课程记录显示名称与学分（停开只是不能新增修读）。
	out, _, _ = runCLI(t, file, "show", " s1 ")
	if !strings.Contains(out, "要求  r1  -> 课程  c1 《大学物理》3 学分") {
		t.Fatalf("show 应显示实际指向课程的名称与学分，out=%q", out)
	}
	// 后续满足途径仍按既有规则：停开课程不能新增修读。
	out, errText, code = runCLI(t, file, "enroll", " s1 ", " r1 ", "2024春", "e1")
	if code != exitRejected || !strings.Contains(errText, "停开") {
		t.Fatalf("停开课程应不能新增修读，code=%d out=%q err=%q", code, out, errText)
	}
}
