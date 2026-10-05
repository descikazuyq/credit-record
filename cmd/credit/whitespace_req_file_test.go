package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“req 按用户给出的完整学生编号、要求编号与课程
// 编号确定要求归属”。记录文件可以合法保存带前后空白（普通空格、制表符、
// 全角空格）的编号，查询与选课登记能把它们区分，登记课程要求也必须如此：
//   - 给 " s1 " 登记指向 " c1 " 的要求 " r1 " 只落在这名学生名下，三个
//     编号原文保留；show 显示完整要求编号与实际指向课程的名称、学分，
//     其他学生的要求与学分保持原样；
//   - 完整学生编号或完整课程编号找不到时按业务拒绝（退出码 1），标准
//     错误点名是哪个对象不存在，标准输出不出现登记成功或返回原要求的
//     提示；即使去掉空白能碰上另一条记录也不借用、不自动创建学生或课程，
//     记录文件逐字节保持原样；
//   - 空字符串编号仍拒绝，只含空白的要求编号不得建立；
//   - 重复登记与冲突判断按完整编号办理：同一学生名下 "r1" 与 " r1 " 是
//     两项要求（仍须指向不同课程），同一学生不能换个要求编号为同一门
//     课程再建要求；完整编号完全相同的重复登记返回原记录且不改写文件，
//     同一完整要求编号改指另一门课程整次拒绝并保留原指向；
//   - 课程停开不改变建立要求的既有规则，新要求本身不带来学分。

// whitespaceReqCLIRecord 构造合法记录：学生 "s1" 与 " s1 "，课程 "c1"
// （4 学分）与 " c1 "（2 学分）；"s1" 名下已有要求 "r1" 指向 "c1"。
func whitespaceReqCLIRecord() *diskRecord {
	return &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: " c1 ", Name: "大学物理", Credit: 2, Open: true},
		},
		Students: []diskStudent{
			{ID: "s1"},
			{ID: " s1 "},
		},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
		},
	}
}

// TestCLIReqWhitespaceIDsHitExactOwner 为 " s1 " 登记指向 " c1 " 的要求
// " r1 "：只落在该学生名下，三个编号原文保留；show 显示完整要求编号与
// 实际指向课程的名称、学分；"s1" 的要求与学分保持原样。
func TestCLIReqWhitespaceIDsHitExactOwner(t *testing.T) {
	file, _ := writeDiskRecord(t, whitespaceReqCLIRecord())

	out, errText, code := runCLI(t, file, "req", " s1 ", " r1 ", " c1 ")
	if code != 0 || !strings.Contains(out, "已为学生  s1  登记要求  r1 ，指向课程  c1 ") {
		t.Fatalf("为 \" s1 \" 登记 \" r1 \" 应成功并保留完整编号，code=%d out=%q err=%q",
			code, out, errText)
	}

	// show 中的归属与成功提示一致：" r1 " 出现在 " s1 " 名下，指向的
	// 是 " c1 "（大学物理 2 学分）而不是 "c1"（高等数学 4 学分）。
	out, _, code = runCLI(t, file, "show", " s1 ")
	if code != 0 || !strings.Contains(out, "要求  r1  -> 课程  c1 《大学物理》2 学分") {
		t.Fatalf("\" s1 \" 名下应列出 \" r1 \" 指向 \" c1 \"，code=%d out=%q", code, out)
	}
	// 新要求本身不带来学分。
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[ r1 ]") {
		t.Fatalf("\" s1 \" 应仍为 0 学分且 \" r1 \" 未满足，code=%d out=%q", code, out)
	}

	// "s1" 保持原样：只有 "r1" 指向 "c1"，学分不变。
	out, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(out, "要求 r1 -> 课程 c1《高等数学》4 学分") ||
		strings.Contains(out, "要求  r1 ") || strings.Contains(out, "大学物理") {
		t.Fatalf("\"s1\" 的要求应保持原样，out=%q", out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("\"s1\" 应仍为 0 学分且 r1 未满足，code=%d out=%q", code, out)
	}
}

// TestCLIReqWhitespaceReqIDIndependent 同一学生名下 "r1" 与 " r1 " 是两项
// 独立要求（仍须指向不同课程）；同一学生换个要求编号为同一门课程再建要求
// 仍拒绝；完整编号完全相同的重复登记返回原记录且不改写文件；同一完整要求
// 编号改指另一门课程整次拒绝并保留原指向。
func TestCLIReqWhitespaceReqIDIndependent(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c2", Name: "大学物理", Credit: 2, Open: true},
		},
		Students: []diskStudent{
			{ID: "s1"},
		},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
		},
	}
	file, _ := writeDiskRecord(t, d)

	// " r1 " 与 "r1" 是两项要求：指向另一门课程 c2 时允许建立。
	out, errText, code := runCLI(t, file, "req", "s1", " r1 ", "c2")
	if code != 0 || !strings.Contains(out, "已为学生 s1 登记要求  r1 ，指向课程 c2") {
		t.Fatalf("登记 \" r1 \" 应作为独立要求被接受，code=%d out=%q err=%q",
			code, out, errText)
	}
	show, _, _ := runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "要求 r1 -> 课程 c1《高等数学》4 学分") ||
		!strings.Contains(show, "要求  r1  -> 课程 c2《大学物理》2 学分") {
		t.Fatalf("两项要求应各自保留，out=%q", show)
	}

	// 同一学生换个要求编号为同一门课程 c1 再建要求：拒绝。
	before := mustReadRecord(t, file)
	out, errText, code = runCLI(t, file, "req", "s1", " r2 ", "c1")
	if code != exitRejected || !strings.Contains(errText, "不能重复建立") ||
		strings.Contains(out, "登记要求") {
		t.Fatalf("同一课程换编号再建要求应拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	assertRecordUnchanged(t, file, before, "同一课程换编号拒绝：")

	// 完整编号完全相同的重复登记：返回原记录，不新增、不改写文件。
	out, _, code = runCLI(t, file, "req", "s1", " r1 ", "c2")
	if code != 0 || !strings.Contains(out, "已存在且内容一致，返回原记录") {
		t.Fatalf("重复登记 \" r1 \" 应幂等返回原记录，code=%d out=%q", code, out)
	}
	assertRecordUnchanged(t, file, before, "幂等重复登记：")

	// 同一完整要求编号改指另一门课程：整次拒绝，保留原指向。
	out, errText, code = runCLI(t, file, "req", "s1", " r1 ", "c1")
	if code != exitRejected || !strings.Contains(errText, "不能改指课程") ||
		strings.Contains(out, "登记要求") {
		t.Fatalf("同一要求编号改指课程应拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	assertRecordUnchanged(t, file, before, "改指课程拒绝：")
	show, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(show, "要求  r1  -> 课程 c2《大学物理》2 学分") {
		t.Fatalf("原指向 c2 应保留，out=%q", show)
	}
}

// TestCLIReqWhitespaceIDRejected 完整学生编号或完整课程编号找不到时按业务
// 拒绝：退出码 1，标准错误点名是哪个对象不存在，标准输出不出现登记成功
// 或返回原要求的提示；即使去掉空白能碰上另一条记录也不借用、不自动创建
// 学生或课程；普通空格、制表符、全角空格一样不能被忽略；空字符串编号与
// 只含空白的要求编号同样拒绝；记录文件逐字节保持原样。
func TestCLIReqWhitespaceIDRejected(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
		},
		Students: []diskStudent{
			{ID: "s1"},
		},
	}

	cases := []struct {
		label   string
		args    []string
		wantErr []string
	}{
		// 学生编号带前后空白：文件中只有 "s1"，应明确报告学生不存在。
		{"学生前导空格", []string{"req", " s1", "r1", "c1"}, []string{"学生", "不存在"}},
		{"学生尾随空格", []string{"req", "s1 ", "r1", "c1"}, []string{"学生", "不存在"}},
		{"学生制表符", []string{"req", "\ts1\t", "r1", "c1"}, []string{"学生", "不存在"}},
		{"学生全角空格", []string{"req", "　s1　", "r1", "c1"}, []string{"学生", "不存在"}},
		// 课程编号带前后空白：文件中只有 "c1"，应明确报告课程不存在。
		{"课程前导空格", []string{"req", "s1", "r1", " c1"}, []string{"课程", "不存在"}},
		{"课程尾随空格", []string{"req", "s1", "r1", "c1 "}, []string{"课程", "不存在"}},
		{"课程制表符", []string{"req", "s1", "r1", "\tc1\t"}, []string{"课程", "不存在"}},
		{"课程全角空格", []string{"req", "s1", "r1", "　c1　"}, []string{"课程", "不存在"}},
		// 空字符串编号仍拒绝。
		{"学生空字符串", []string{"req", "", "r1", "c1"}, []string{"不能为空"}},
		{"要求空字符串", []string{"req", "s1", "", "c1"}, []string{"不能为空"}},
		{"课程空字符串", []string{"req", "s1", "r1", ""}, []string{"不能为空"}},
		// 只含空白的要求编号不得建立。
		{"要求编号全空格", []string{"req", "s1", "  ", "c1"}, []string{"要求编号"}},
		{"要求编号全角空格", []string{"req", "s1", "　", "c1"}, []string{"要求编号"}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			file, raw := writeDiskRecord(t, d)
			out, errText, code := runCLI(t, file, tc.args...)
			if code != exitRejected {
				t.Fatalf("%s：应业务拒绝（退出码 1），code=%d out=%q err=%q",
					tc.label, code, out, errText)
			}
			if strings.Contains(out, "登记要求") || strings.Contains(out, "返回原记录") {
				t.Fatalf("%s：被拒绝时不应输出登记成功或返回原要求的提示，out=%q", tc.label, out)
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

// TestCLIReqWhitespaceClosedCourse 课程停开不改变建立要求的既有规则：
// 为停开课程登记新要求仍按原规则办理，新要求本身不带来学分。
func TestCLIReqWhitespaceClosedCourse(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: " c1 ", Name: "大学物理", Credit: 2, Open: false},
		},
		Students: []diskStudent{
			{ID: " s1 "},
		},
	}
	file, _ := writeDiskRecord(t, d)

	out, errText, code := runCLI(t, file, "req", " s1 ", " r1 ", " c1 ")
	if code != 0 || !strings.Contains(out, "已为学生  s1  登记要求  r1 ，指向课程  c1 ") {
		t.Fatalf("课程停开不应改变建立要求的规则，code=%d out=%q err=%q", code, out, errText)
	}
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(out, "总学分：0") {
		t.Fatalf("新要求本身不带来学分，code=%d out=%q", code, out)
	}
}
