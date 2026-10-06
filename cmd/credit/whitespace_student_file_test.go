package main

import (
	"os"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“student 按用户给出的完整编号确定学生身份”。
// 登记学生与登记课程要求、提交成绩、核对、查询使用同一套对象：
//   - “s1”与“ s1 ”是两名独立学生：文件中只有“s1”时登记“ s1 ”新建
//     后者，成功提示对应这名学生并显示完整编号，不借用前者的要求、修读、
//     免修或学分；只有“ s1 ”时登记“s1”同样新建独立记录；
//   - 两种编号都已存在时，再次登记任意一个完整编号都准确命中本人，
//     提示已有记录、退出码 0，不新增副本、不改动记录文件，即使两人
//     名下有相同编号的要求、修读或免修也不移动、不合并；
//   - 新登记的带空白编号可直接用于 show 与 check：尚无要求时查看显示
//     没有要求，核对显示总学分为零；编号相近的另一名学生仍显示自己的
//     记录；
//   - 空字符串与全部由空白字符组成（普通空格、制表符、全角空格、
//     不换行空格混用）的编号以退出码 1 拒绝，标准错误说明学生编号不能
//     为空，标准输出不出现登记成功或返回原学生的提示，不创建学生，
//     已有文件逐字节保持原样。

// studentCLIRecord 构造合法记录：学生 "s1" 名下有要求 r1（4 学分课程
// c1）、已通过修读 e1；没有 " s1 " 这名学生。
func studentCLIRecord() *diskRecord {
	return &diskRecord{
		Version: 1,
		Courses: []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students: []diskStudent{
			{ID: "s1"},
		},
		Requirements: []diskReq{
			{ID: "r1", Student: "s1", Course: "c1"},
		},
		Enrollments: []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "passed", ResultSeq: 1},
		},
		NextResultSeq: 1,
	}
}

// TestCLIStudentWhitespaceCreatesDistinct 文件中只有 "s1"（且名下已有
// 要求与通过修读）时，登记 " s1 " 必须新建另一名学生：成功提示显示
// 完整编号；新生没有任何要求、修读、免修，核对 0 学分；"s1" 的记录
// 与学分保持原样。
func TestCLIStudentWhitespaceCreatesDistinct(t *testing.T) {
	file, _ := writeDiskRecord(t, studentCLIRecord())

	out, errText, code := runCLI(t, file, "student", " s1 ")
	if code != 0 || !strings.Contains(out, "已登记学生  s1 \n") {
		t.Fatalf("登记 \" s1 \" 应成功且提示完整编号，code=%d out=%q err=%q",
			code, out, errText)
	}
	if strings.Contains(out, "已存在") {
		t.Fatalf("登记新学生不应提示已存在，out=%q", out)
	}

	// 新生：show 列出学生本人且没有任何记录，check 总学分为零。
	out, _, code = runCLI(t, file, "show", " s1 ")
	if code != 0 || !strings.HasPrefix(out, "学生  s1 \n") ||
		!strings.Contains(out, "课程要求：（无）") ||
		!strings.Contains(out, "修读：（无）") ||
		!strings.Contains(out, "免修历史：（无）") {
		t.Fatalf("新生 show 应只有空记录，code=%d out=%q", code, out)
	}
	if strings.Contains(out, "r1") || strings.Contains(out, "e1") {
		t.Fatalf("新生不应继承 \"s1\" 的要求或修读，out=%q", out)
	}
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "课程要求：（无）") {
		t.Fatalf("新生核对应显示总学分为零且没有要求，code=%d out=%q", code, out)
	}

	// "s1" 仍显示自己的要求 r1、通过修读 e1 与 4 学分。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 || !strings.Contains(out, "要求 r1 -> 课程 c1") ||
		!strings.Contains(out, "修读 e1") {
		t.Fatalf("\"s1\" 的原有记录应保持不变，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("\"s1\" 应仍为 4 学分，code=%d out=%q", code, out)
	}
}

// TestCLIStudentWhitespaceReverseOnlyPaddedExists 文件中只有 " s1 "
// （名下有要求与通过修读）时登记 "s1" 也新建独立记录，不借用前者资料。
func TestCLIStudentWhitespaceReverseOnlyPaddedExists(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{{ID: "c1", Name: "高等数学", Credit: 4, Open: true}},
		Students: []diskStudent{
			{ID: " s1 "},
		},
		Requirements: []diskReq{
			{ID: "r1", Student: " s1 ", Course: "c1"},
		},
		Enrollments: []diskEnr{
			{ID: "e1", Student: " s1 ", Req: "r1", Term: "2024春", Result: "passed", ResultSeq: 1},
		},
		NextResultSeq: 1,
	}
	file, _ := writeDiskRecord(t, d)

	if out, _, code := runCLI(t, file, "student", "s1"); code != 0 ||
		!strings.Contains(out, "已登记学生 s1\n") {
		t.Fatalf("只有 \" s1 \" 时登记 \"s1\" 应新建独立记录，code=%d out=%q", code, out)
	}
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") {
		t.Fatalf("新建的 \"s1\" 应为 0 学分，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", " s1 ")
	if code != 0 || !strings.Contains(out, "总学分：4") {
		t.Fatalf("\" s1 \" 的 4 学分应保持原样，code=%d out=%q", code, out)
	}
}

// TestCLIStudentWhitespaceIdempotentHitsExactOwner 两名学生都已存在且
// 名下有相同编号的要求、修读时，重复登记任一完整编号只命中本人：提示
// 已有记录、退出码 0、文件逐字节不变；历史不移动、不合并。
func TestCLIStudentWhitespaceIdempotentHitsExactOwner(t *testing.T) {
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
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "passed", ResultSeq: 1},
			{ID: "e1", Student: " s1 ", Req: "r1", Term: "2024春", Result: "enrolled"},
		},
		NextResultSeq: 1,
	}
	file, raw := writeDiskRecord(t, d)

	cases := []struct {
		label string
		id    string
	}{
		{"普通编号", "s1"},
		{"带空白编号", " s1 "},
		{"再次带空白编号", " s1 "},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			out, errText, code := runCLI(t, file, "student", tc.id)
			if code != 0 {
				t.Fatalf("重复登记 %q 应成功确认，code=%d err=%q", tc.id, code, errText)
			}
			if !strings.Contains(out, "学生 "+tc.id+" 已存在，返回原记录") {
				t.Fatalf("应提示 %q 已有记录且显示完整编号，out=%q", tc.id, out)
			}
			if strings.Contains(out, "已登记学生") {
				t.Fatalf("重复登记不应出现新建提示，out=%q", out)
			}
			assertFileByteIdentical(t, file, raw, tc.label+"：")
		})
	}

	// 两人的同号历史仍各自归属：" s1 " 的 e1 是选课、0 学分；"s1"
	// 的 e1 已通过、4 学分。
	out, _, _ := runCLI(t, file, "show", " s1 ")
	if !strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("\" s1 \" 的 e1 应仍是选课，out=%q", out)
	}
	out, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：通过") {
		t.Fatalf("\"s1\" 的 e1 应仍是通过，out=%q", out)
	}
	if out, _, code := runCLI(t, file, "check", " s1 "); code != 0 ||
		!strings.Contains(out, "总学分：0") {
		t.Fatalf("\" s1 \" 应仍为 0 学分，code=%d out=%q", code, out)
	}
	if out, _, code := runCLI(t, file, "check", "s1"); code != 0 ||
		!strings.Contains(out, "总学分：4") {
		t.Fatalf("\"s1\" 应仍为 4 学分，code=%d out=%q", code, out)
	}
}

// TestCLIStudentWhitespaceNewFilePersistence 文件不存在时直接登记带空白
// 编号：完整编号落盘，再次登记幂等命中本人，去掉空白的编号查不到。
func TestCLIStudentWhitespaceNewFilePersistence(t *testing.T) {
	file := t.TempDir() + "/records.json"

	out, _, code := runCLI(t, file, "student", " s1 ")
	if code != 0 || !strings.Contains(out, "已登记学生  s1 ") ||
		!strings.Contains(out, "从空记录开始") {
		t.Fatalf("新文件登记 \" s1 \" 应成功，code=%d out=%q", code, out)
	}

	before := mustReadRecord(t, file)
	out, _, code = runCLI(t, file, "student", " s1 ")
	if code != 0 || !strings.Contains(out, "学生  s1  已存在，返回原记录") {
		t.Fatalf("再次登记应幂等命中 \" s1 \"，code=%d out=%q", code, out)
	}
	assertRecordUnchanged(t, file, before, "幂等重复登记：")

	// 文件中只有 " s1 "：用 "s1" 查询应明确报不存在，不能借到同一记录。
	_, errText, code := runCLI(t, file, "show", "s1")
	if code != exitRejected || !strings.Contains(errText, "学生 s1 不存在") {
		t.Fatalf("\"s1\" 应查不到 \" s1 \"，code=%d err=%q", code, errText)
	}
}

// TestCLIStudentBlankIDRejected 空字符串与全部由空白字符组成（普通空格、
// 制表符、全角空格、不换行空格混用）的编号一律退出码 1：标准错误说明
// 学生编号不能为空，标准输出没有任何成功或返回原学生的提示，不创建学生，
// 已有文件逐字节保持原样。
func TestCLIStudentBlankIDRejected(t *testing.T) {
	cases := []struct {
		label string
		id    string
	}{
		{"空字符串", ""},
		{"普通空格", " "},
		{"制表符", "\t"},
		{"全角空格", "　"},
		{"不换行空格", " "},
		{"换行回车", "\n\r"},
		{"混合空白", " \t　 \n\r"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			f, raw := writeDiskRecord(t, studentCLIRecord())
			out, errText, code := runCLI(t, f, "student", tc.id)
			if code != exitRejected {
				t.Fatalf("应业务拒绝（退出码 1），code=%d out=%q err=%q",
					code, out, errText)
			}
			if !strings.Contains(errText, "学生编号不能为空") {
				t.Fatalf("标准错误应说明学生编号不能为空，err=%q", errText)
			}
			if strings.Contains(out, "已登记学生") || strings.Contains(out, "已存在") {
				t.Fatalf("被拒绝时标准输出不应出现登记或返回提示，out=%q", out)
			}
			assertFileByteIdentical(t, f, raw, tc.label+"：")
		})
	}

	// 全新文件也不会因空白编号被创建。
	dir := t.TempDir()
	emptyFile := dir + "/empty.json"
	if _, _, code := runCLI(t, emptyFile, "student", " 　 \t "); code != exitRejected {
		t.Fatalf("空白编号应拒绝，code=%d", code)
	}
	if exists := fileExists(emptyFile); exists {
		t.Fatal("空白编号被拒后不应创建记录文件")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
