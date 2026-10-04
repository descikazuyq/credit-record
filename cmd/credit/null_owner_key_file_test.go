package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归含 U+0000 编号的要求归属：
//   - ("s","x\x00r") 与 ("s\x00x","r") 在旧的 student+"\x00"+id 拼接键下
//     完全同键，但分属两名学生：合法文件必须正常打开，check/show 各自得到
//     本人的课程、要求、免修依据与学分（4 与 3），不报编号重复；
//   - 文件里只有 "s\x00x" 的要求 "r"，却有 "s" 的一份声称取代 "x\x00r"
//     的有效免修时，必须按损坏文件退出码 2 处理，不输出成功核对结果，原文件
//     不改；已拒绝申请指向该不存在目标仍合法保留。
//
// 编号经 JSON 序列化后写成 "\u0000" 转义，解码后才是空字符，与真实记录文件
// 的写法一致。

// nulOwnersDiskRecord 构造两名学生各自完整合法的记录。
func nulOwnersDiskRecord() *diskRecord {
	return &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c4", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c3", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s"}, {ID: "s\x00x"}},
		Requirements: []diskReq{
			{ID: "x\x00r", Student: "s", Course: "c4"},
			{ID: "r", Student: "s\x00x", Course: "c3"},
		},
		Enrollments: []diskEnr{
			{ID: "x\x00e", Student: "s", Req: "x\x00r", Term: "2024春",
				Result: "passed", ResultSeq: 1},
			{ID: "e", Student: "s\x00x", Req: "r", Term: "2024春",
				Result: "passed", ResultSeq: 2},
		},
		Waivers: []diskWaiver{
			{ID: "x\x00w", Student: "s", Req: "x\x00r",
				Basis: "s 的竞赛获奖", Status: "approved"},
			{ID: "w", Student: "s\x00x", Req: "r",
				Basis: "s\x00x 的外校修读证明", Status: "approved"},
		},
		NextResultSeq: 2,
	}
}

// TestCLINullContainingIDsKeepSeparateOwnership 主场景：两组含空字符的归属
// 各自合法，check 分别得到 4 与 3 学分，来源、课程与 show 历史各归各，
// 文件不被误判损坏、只读访问不改文件。
func TestCLINullContainingIDsKeepSeparateOwnership(t *testing.T) {
	file, raw := writeDiskRecord(t, nulOwnersDiskRecord())

	out, errText, code := runCLI(t, file, "check", "s")
	if code != 0 {
		t.Fatalf("s 的合法记录应正常打开核对，code=%d out=%q err=%q", code, out, errText)
	}
	for _, want := range []string{
		"总学分：4",
		"课程 c4《高等数学》，4 学分",
		"已满足",
		"另有通过修读历史",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("s 的核对结果应含 %q，out=%q", want, out)
		}
	}
	// 只能出现 s 本人 4 学分课程，不能串到 s\x00x 的 3 学分课程。
	if strings.Contains(out, "c3") || strings.Contains(out, "线性代数") ||
		strings.Contains(out, "总学分：3") {
		t.Fatalf("s 的核对结果串到了另一名学生的课程，out=%q", out)
	}
	if !strings.Contains(out, "来源为有效免修 ") {
		t.Fatalf("s 的要求应以免修说明来源，out=%q", out)
	}

	out, errText, code = runCLI(t, file, "check", "s\x00x")
	if code != 0 {
		t.Fatalf("s\\x00x 的合法记录应正常核对，code=%d out=%q err=%q", code, out, errText)
	}
	for _, want := range []string{
		"总学分：3",
		"课程 c3《线性代数》，3 学分",
		"已满足",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("s\\x00x 的核对结果应含 %q，out=%q", want, out)
		}
	}
	if strings.Contains(out, "c4") || strings.Contains(out, "高等数学") {
		t.Fatalf("s\\x00x 的核对结果串到了 s 的课程，out=%q", out)
	}

	// show 历史各归各：本人的修读、免修、依据可见，对方记录不可见。
	show, _, code := runCLI(t, file, "show", "s")
	if code != 0 {
		t.Fatalf("show s 应成功，code=%d out=%q", code, show)
	}
	if !strings.Contains(show, "要求 ") || !strings.Contains(show, " -> 课程 c4《高等数学》4 学分") ||
		!strings.Contains(show, `依据 "s 的竞赛获奖"，状态：有效`) {
		t.Fatalf("show s 应保留本人要求与免修依据，out=%q", show)
	}
	if strings.Contains(show, "c3") || strings.Contains(show, "外校修读证明") {
		t.Fatalf("show s 不能出现 s\\x00x 的记录，out=%q", show)
	}

	showX, _, code := runCLI(t, file, "show", "s\x00x")
	if code != 0 {
		t.Fatalf("show s\\x00x 应成功，code=%d out=%q", code, showX)
	}
	if !strings.Contains(showX, " -> 课程 c3《线性代数》3 学分") ||
		!strings.Contains(showX, `依据 "s\x00x 的外校修读证明"，状态：有效`) {
		t.Fatalf("show s\\x00x 应保留本人要求、含空字符的依据原文，out=%q", showX)
	}
	if strings.Contains(showX, "c4") || strings.Contains(showX, "s 的竞赛获奖") {
		t.Fatalf("show s\\x00x 不能出现 s 的记录，out=%q", showX)
	}

	assertFileByteIdentical(t, file, raw, "含空字符编号的合法记录只读之后：")
}

// TestCLIApprovedWaiverBorrowingNullConfusableReqRejectsFile 文件里只有
// "s\x00x" 的要求 "r"，却有一份属于 "s"、声称取代 "x\x00r" 的有效免修：
// 旧实现会顺着同键要求借用他人记录合法化。修复后任何访问都退出码 2，
// stdout 无业务结果，错误明确说明目标不属于该学生或不存在，原文件保留。
func TestCLIApprovedWaiverBorrowingNullConfusableReqRejectsFile(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c4", Name: "高等数学", Credit: 4, Open: true},
			{ID: "c3", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s"}, {ID: "s\x00x"}},
		Requirements: []diskReq{
			{ID: "r", Student: "s\x00x", Course: "c3"},
		},
		Waivers: []diskWaiver{
			{ID: "w", Student: "s\x00x", Req: "r",
				Basis: "s\x00x 本人的依据", Status: "approved"},
			{ID: "x\x00w", Student: "s", Req: "x\x00r",
				Basis: "s 的材料", Status: "approved"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"check 免修所属学生", []string{"check", "s"}},
		{"show 免修所属学生", []string{"show", "s"}},
		{"check 要求持有者", []string{"check", "s\x00x"}},
		{"list-courses", []string{"list-courses"}},
		{"写入类命令", []string{"student", "s9"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errText, code := runCLI(t, file, tc.args...)
			if code != exitFile {
				t.Fatalf("借用他人要求的有效免修必须按损坏文件退出码 %d 处理，code=%d out=%q err=%q",
					exitFile, code, out, errText)
			}
			if out != "" {
				t.Fatalf("损坏文件不得输出成功核对或登记结果，out=%q", out)
			}
			for _, want := range []string{"内容损坏", "x\x00w", "不存在或不属于该学生"} {
				if !strings.Contains(errText, want) {
					t.Fatalf("错误应点名免修并说明目标要求不属于该学生或不存在（缺 %q），err=%q",
						want, errText)
				}
			}
		})
	}
	assertFileByteIdentical(t, file, raw, "拒绝读取损坏文件之后：")
}

// TestCLINullConfusableRejectedWaiverStaysValidHistory 同样的跨学生目标，
// 若只是被拒绝的申请：记录合法可打开，s 学分为零且列出该拒绝历史，
// s\x00x 本人的 3 学分核对不受影响。
func TestCLINullConfusableRejectedWaiverStaysValidHistory(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c3", Name: "线性代数", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s"}, {ID: "s\x00x"}},
		Requirements: []diskReq{
			{ID: "r", Student: "s\x00x", Course: "c3"},
		},
		Waivers: []diskWaiver{
			{ID: "w", Student: "s\x00x", Req: "r",
				Basis: "s\x00x 本人的依据", Status: "approved"},
			{ID: "x\x00w", Student: "s", Req: "x\x00r", Basis: "s 的材料",
				Status: "rejected", Reason: "目标要求 x\x00r 不存在或不属于该学生"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s")
	if code != 0 {
		t.Fatalf("含被拒绝申请的文件应正常打开，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：0") || !strings.Contains(out, "被拒绝的免修：") {
		t.Fatalf("s 应 0 学分且列出被拒绝申请，out=%q", out)
	}

	out, _, code = runCLI(t, file, "check", "s\x00x")
	if code != 0 || !strings.Contains(out, "总学分：3") {
		t.Fatalf("s\\x00x 本人的 3 学分核对不受影响，code=%d out=%q", code, out)
	}
	assertFileByteIdentical(t, file, raw, "只读之后：")
}
