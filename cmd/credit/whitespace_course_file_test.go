package main

import (
	"strings"
	"testing"
)

// 本文件从命令行入口回归“course 登记按用户给出的完整编号区分课程”。
// 课程编号与要求、查询使用同一口径：普通空格、制表符、全角空格都是
// 编号内容，绝不修剪：
//   - 首次分别登记 "c1" 与 " c1 " 保存两门独立课程，各自保留完整编号、
//     名称与正整数学分，初始开放，课程列表分别显示；
//   - 文件中只有 "c1" 时提交 " c1 " 新建后者，不借用前者的资料或停开
//     状态；已有文件中的带空白编号课程同样按原编号维护；
//   - 再次提交某个完整编号只判断对应课程：同内容幂等返回原记录，内容
//     不同原地更新，不新增副本、不改动另一门课程；
//   - 学分修改限制跟随实际命中的课程：要求引用了 " c1 " 时该课程不能
//     改学分，连同新名称整次拒绝（退出码 1），原名称、学分、开放状态
//     全部保留；仅 "c1" 被引用不能阻止 " c1 " 更新，反之亦然；
//   - 被引用课程保持原学分时仍允许改名，核对的满足情况、学分来源与
//     总学分不因此变化；重新登记停开课程后仍须停开；
//   - 空字符串与全部由空白字符组成的课程编号仍拒绝：退出码 1，标准
//     错误说明原因，标准输出不提示登记或更新成功，记录文件不变。

// TestCLICourseWhitespaceDistinctCourses 首次分别登记 "c1" 与 " c1 "：
// 两门独立课程，各自保留完整编号、名称与学分，初始开放，列表分别显示。
func TestCLICourseWhitespaceDistinctCourses(t *testing.T) {
	file := tempRecordFile(t)

	out, errText, code := runCLI(t, file, "course", "c1", "高等数学", "4")
	if code != 0 || !strings.Contains(out, "已登记课程 c1《高等数学》4 学分，状态：开放") {
		t.Fatalf("登记 \"c1\" 应成功，code=%d out=%q err=%q", code, out, errText)
	}
	out, errText, code = runCLI(t, file, "course", " c1 ", "大学物理", "3")
	if code != 0 || !strings.Contains(out, "已登记课程  c1 《大学物理》3 学分，状态：开放") {
		t.Fatalf("\" c1 \" 应作为新课程登记并保留完整编号，code=%d out=%q err=%q",
			code, out, errText)
	}

	out, _, code = runCLI(t, file, "list-courses")
	if code != 0 ||
		!strings.Contains(out, "课程 c1《高等数学》4 学分，状态：开放") ||
		!strings.Contains(out, "课程  c1 《大学物理》3 学分，状态：开放") {
		t.Fatalf("课程列表应分别显示两门课程及其完整编号，code=%d out=%q", code, out)
	}
}

// TestCLICourseWhitespaceNoBorrowFromTrimmed 文件中只有 "c1"（停开）时，
// 提交 " c1 " 必须新建后者：初始开放、使用本次提交的名称与学分，不借用
// "c1" 的资料或停开状态；"c1" 保持原样。
func TestCLICourseWhitespaceNoBorrowFromTrimmed(t *testing.T) {
	file, _ := writeDiskRecord(t, &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: false},
		},
	})

	out, errText, code := runCLI(t, file, "course", " c1 ", "大学物理", "3")
	if code != 0 || !strings.Contains(out, "已登记课程  c1 《大学物理》3 学分，状态：开放") {
		t.Fatalf("文件中只有 \"c1\" 时 \" c1 \" 应新建并初始开放，code=%d out=%q err=%q",
			code, out, errText)
	}
	if strings.Contains(out, "已更新") || strings.Contains(out, "返回原记录") {
		t.Fatalf("\" c1 \" 不应命中 \"c1\" 的记录，out=%q", out)
	}

	out, _, code = runCLI(t, file, "list-courses")
	if code != 0 ||
		!strings.Contains(out, "课程 c1《高等数学》4 学分，状态：停开") ||
		!strings.Contains(out, "课程  c1 《大学物理》3 学分，状态：开放") {
		t.Fatalf("两门课程应各自保留资料与状态，code=%d out=%q", code, out)
	}
}

// TestCLICourseWhitespaceExactIdempotentAndUpdate 再次提交某个完整编号只
// 判断对应课程：同内容幂等返回原记录、不改文件；内容不同原地更新，不
// 新增副本、不改动另一门课程；重新登记停开课程后仍须停开。
func TestCLICourseWhitespaceExactIdempotentAndUpdate(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"course", "c1", "高等数学", "4"},
		{"course", " c1 ", "大学物理", "3"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 同完整编号同内容：幂等返回原记录，文件不变。
	before := mustReadRecord(t, file)
	out, errText, code := runCLI(t, file, "course", " c1 ", "大学物理", "3")
	if code != 0 || !strings.Contains(out, "已存在且内容一致，返回原记录") {
		t.Fatalf("完整编号同内容应幂等返回原记录，code=%d out=%q err=%q", code, out, errText)
	}
	assertRecordUnchanged(t, file, before, "幂等重复登记")

	// 内容不同：原地更新 " c1 "，不新增副本、不改动 "c1"。
	out, errText, code = runCLI(t, file, "course", " c1 ", "大学物理（上）", "5")
	if code != 0 || !strings.Contains(out, "已更新") {
		t.Fatalf("未被引用的 \" c1 \" 应允许更新，code=%d out=%q err=%q", code, out, errText)
	}
	out, _, _ = runCLI(t, file, "list-courses")
	if !strings.Contains(out, "课程  c1 《大学物理（上）》5 学分，状态：开放") ||
		!strings.Contains(out, "课程 c1《高等数学》4 学分，状态：开放") {
		t.Fatalf("更新应只命中 \" c1 \"，另一门课程保持原样，out=%q", out)
	}
	if strings.Count(out, "课程  c1 《") != 1 || strings.Count(out, "课程 c1《") != 1 {
		t.Fatalf("更新不应新增副本，out=%q", out)
	}

	// 停开 " c1 " 后重新登记：仍须停开。
	if _, errText, code := runCLI(t, file, "course-close", " c1 "); code != 0 {
		t.Fatalf("停开 \" c1 \" 应成功，code=%d err=%q", code, errText)
	}
	out, errText, code = runCLI(t, file, "course", " c1 ", "大学物理（下）", "6")
	if code != 0 || !strings.Contains(out, "状态保持：停开") {
		t.Fatalf("重新登记停开课程后仍须停开，code=%d out=%q err=%q", code, out, errText)
	}
	out, _, _ = runCLI(t, file, "list-courses")
	if !strings.Contains(out, "课程  c1 《大学物理（下）》6 学分，状态：停开") {
		t.Fatalf("停开状态应保持，out=%q", out)
	}
}

// TestCLICourseWhitespaceCreditLockFollowsHitCourse 学分修改限制跟随实际
// 命中的课程：要求引用 " c1 " 时，" c1 " 连同新名称和不同学分整次拒绝
// （退出码 1），原名称、学分、开放状态与记录文件全部保留；未被引用的
// "c1" 仍可更新。已有文件中的带空白编号课程得到同样处理。
func TestCLICourseWhitespaceCreditLockFollowsHitCourse(t *testing.T) {
	file, _ := writeDiskRecord(t, &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
			{ID: " c1 ", Name: "大学物理", Credit: 3, Open: true},
		},
		Students: []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: " c1 "},
		},
	})

	// " c1 " 被引用：新名称 + 不同学分整次拒绝。
	before := mustReadRecord(t, file)
	out, errText, code := runCLI(t, file, "course", " c1 ", "大学物理（下）", "5")
	if code != exitRejected {
		t.Fatalf("被引用的 \" c1 \" 改学分应整次拒绝（退出码 1），code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(errText, "已被课程要求引用") || !strings.Contains(errText, "原学分 3") {
		t.Fatalf("拒绝说明应指出已被要求引用及原学分 3，err=%q", errText)
	}
	if strings.Contains(out, "已登记课程") || strings.Contains(out, "已更新") ||
		strings.Contains(out, "返回原记录") {
		t.Fatalf("被拒绝时 stdout 不应提示登记或更新成功，out=%q", out)
	}
	assertRecordUnchanged(t, file, before, "被引用课程的整单更新")

	// 原名称、学分、开放状态全部保留。
	out, _, _ = runCLI(t, file, "list-courses")
	if !strings.Contains(out, "课程  c1 《大学物理》3 学分，状态：开放") {
		t.Fatalf("整次拒绝后 \" c1 \" 应保持原样，out=%q", out)
	}

	// 未被引用的 "c1" 不受该限制。
	out, errText, code = runCLI(t, file, "course", "c1", "高等数学（上）", "6")
	if code != 0 || !strings.Contains(out, "已更新") {
		t.Fatalf("未被引用的 \"c1\" 应允许更新，code=%d out=%q err=%q", code, out, errText)
	}
	out, _, _ = runCLI(t, file, "list-courses")
	if !strings.Contains(out, "课程 c1《高等数学（上）》6 学分，状态：开放") ||
		!strings.Contains(out, "课程  c1 《大学物理》3 学分，状态：开放") {
		t.Fatalf("更新应只命中 \"c1\"，\" c1 \" 保持原样，out=%q", out)
	}
}

// TestCLICourseWhitespaceRenameReferencedKeepsCheck 被引用的带空白编号
// 课程保持原学分时仍允许改名：已有要求、修读继续关联原课程，核对的满足
// 情况、学分来源与总学分不因此变化。
func TestCLICourseWhitespaceRenameReferencedKeepsCheck(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", " c1 ", "大学物理", "3"},
		{"req", "s1", "r1", " c1 "},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	out, errText, code := runCLI(t, file, "course", " c1 ", "大学物理（强化班）", "3")
	if code != 0 || !strings.Contains(out, "已更新") {
		t.Fatalf("被引用课程保持原学分改名应成功，code=%d out=%q err=%q", code, out, errText)
	}

	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：3") ||
		!strings.Contains(out, "要求 r1（课程  c1 《大学物理（强化班）》，3 学分）：已满足") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("改名后满足情况、学分来源与总学分不应变化，code=%d out=%q", code, out)
	}
	out, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(out, "要求 r1 -> 课程  c1 《大学物理（强化班）》3 学分") ||
		!strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：通过") {
		t.Fatalf("已有要求与修读应继续关联原课程，out=%q", out)
	}
}

// TestCLICourseWhitespaceIDRejected 空字符串与全部由空白字符组成的课程
// 编号仍拒绝：退出码 1，标准错误说明原因，标准输出不提示登记或更新
// 成功，记录文件逐字节保持原样。
func TestCLICourseWhitespaceIDRejected(t *testing.T) {
	cases := []struct {
		label string
		id    string
	}{
		{"空字符串", ""},
		{"单个空格", " "},
		{"多个空格", "   "},
		{"制表符", "\t"},
		{"全角空格", "　"},
		{"混合空白", " \t　"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			file, raw := writeDiskRecord(t, &diskRecord{
				Version: 1,
				Courses: []diskCourse{
					{ID: "c1", Name: "高等数学", Credit: 4, Open: true},
				},
			})
			out, errText, code := runCLI(t, file, "course", tc.id, "大学物理", "3")
			if code != exitRejected {
				t.Fatalf("编号 %q 应业务拒绝（退出码 1），code=%d out=%q err=%q",
					tc.id, code, out, errText)
			}
			if !strings.Contains(errText, "课程编号") {
				t.Fatalf("错误输出应说明课程编号问题，err=%q", errText)
			}
			if strings.Contains(out, "已登记课程") || strings.Contains(out, "已更新") ||
				strings.Contains(out, "返回原记录") {
				t.Fatalf("被拒绝时 stdout 不应提示登记或更新成功，out=%q", out)
			}
			assertFileByteIdentical(t, file, raw, "拒绝空白编号：")
		})
	}
}
