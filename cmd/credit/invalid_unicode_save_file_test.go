package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“保存时不得悄悄改写文字”。命令行参数在本机上可以
// 携带非法 UTF-8 字节（Go 的字符串按字节保留，不做替换），这些字节会原样
// 进入待保存的编号、名称、学期、依据或原因。标准库序列化会把它们静默替换成
// U+FFFD 却提示成功，因此保存必须明确拒绝这种数据：
//   - 退出码沿用保存失败的 2，标准错误说明保存失败、点名目标文件，并指出
//     无法原样保存的文字所在的记录类别与具体字段；
//   - 标准输出不能出现登记成功、免修已生效、历史已保存或文件已创建等提示；
//   - 已有记录文件逐字节保留、原先能查询的学生与学分照常查询；目标文件原本
//     不存在时不得留下新记录文件或临时文件；
//   - 即使问题只在一条已拒绝免修的文字里，也拒绝整次保存，历史不落盘；
//   - 合法文字（用户确实输入的“�”、中文与补充平面字符、编号中合法的控制
//     字符、依据中的空白、字面上的“\uD800”）继续正常保存使用。

// invalidUTF8Arg 返回一个带非法 UTF-8 字节的参数。
func invalidUTF8Arg(prefix string) string { return prefix + string([]byte{0xFF}) }

// assertSaveRejectsBadText 在“保存失败”通用断言之外，额外要求错误指出无法
// 原样保存的文字、记录类别与具体字段。
func assertSaveRejectsBadText(t *testing.T, file string, args []string, category, field, label string) {
	t.Helper()
	out, errText, code := runCLI(t, file, args...)
	if code != exitFile {
		t.Fatalf("%s：应按保存失败退出码 %d 结束，code=%d out=%q err=%q",
			label, exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("%s：保存失败时标准输出必须为空，out=%q", label, out)
	}
	for _, want := range []string{"保存记录文件", file, "失败", "无法原样保存", category, field} {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s：错误输出应包含 %q（说明保存失败、目标文件、类别 %q 与字段 %q），err=%q",
				label, want, category, field, errText)
		}
	}
}

// TestCLISaveRejectsInvalidUTF8NotCreated 目标文件原本不存在时，待保存文字
// 含非法 UTF-8（学生编号、课程名称、学期、免修依据等）必须拒绝整次保存：
// 退出码 2、标准输出为空，目标文件与临时文件都不得留下。
func TestCLISaveRejectsInvalidUTF8NotCreated(t *testing.T) {
	cases := []struct {
		label    string
		args     []string
		category string
		field    string
	}{
		{"学生编号非法", []string{"student", invalidUTF8Arg("s")}, "学生", "编号"},
		{"课程编号非法", []string{"course", invalidUTF8Arg("c"), "数学", "4"}, "课程", "编号"},
		{"课程名称非法", []string{"course", "c1", invalidUTF8Arg("数学"), "4"}, "课程", "名称"},
		{"学期非法", []string{"enroll", "s1", "r1", invalidUTF8Arg("2024春"), "e1"}, "修读", "学期"},
		{"免修依据非法", []string{"waiver", "s1", "r1", "w9", invalidUTF8Arg("依据")}, "免修", "依据"},
		{"撤销原因非法", []string{"revoke-waiver", "s1", "w1", invalidUTF8Arg("原因")}, "免修", "原因"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "records.json")
			// enroll/waiver/revoke 需要前置记录；这些前置命令本身必须正常落盘。
			for _, st := range [][]string{
				{"student", "s1"},
				{"course", "c1", "数学", "4"},
				{"req", "s1", "r1", "c1"},
				{"waiver", "s1", "r1", "w1", "原始依据"},
			} {
				if _, errText, code := runCLI(t, file, st...); code != 0 {
					t.Fatalf("前置步骤 %v 失败，code=%d err=%q", st, code, errText)
				}
			}
			before, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			assertSaveRejectsBadText(t, file, c.args, c.category, c.field, c.label)

			if _, statErr := os.Stat(file); statErr != nil {
				t.Fatalf("%s：前置已创建的记录文件应继续存在：%v", c.label, statErr)
			}
			after, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatalf("%s：拒绝保存后原文件必须逐字节保留\nwant=%q\n got=%q",
					c.label, before, after)
			}
			leftover, _ := filepath.Glob(filepath.Join(dir, ".credit-*.tmp"))
			if len(leftover) != 0 {
				t.Fatalf("%s：不得残留临时文件：%v", c.label, leftover)
			}
		})
	}
}

// TestCLISaveRejectsInvalidUTF8ForBrandNewFile 目标文件与前置数据都不存在时，
// 第一条登记命令的文字就非法：不得创建任何记录文件，也不能提示已登记或已
// 创建。
func TestCLISaveRejectsInvalidUTF8ForBrandNewFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	assertSaveRejectsBadText(t, file, []string{"student", invalidUTF8Arg("s")},
		"学生", "编号", "全新文件登记非法编号学生")

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("目标文件原本不存在时不得留下新记录文件，stat err=%v", err)
	}
}

// TestCLISaveRejectedWaiverBadTextRefusesWholeSave 因目标要求不存在而本应被
// 拒绝、但要写入免修历史的首次申请，其依据含非法 UTF-8 时也必须拒绝整次
// 保存：不能只跳过历史、不能显示“已拒绝/已记入历史”，退出码 2，恢复后
// 历史中查不到该申请。
func TestCLISaveRejectedWaiverBadTextRefusesWholeSave(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")
	if _, _, code := runCLI(t, file, "student", "s1"); code != 0 {
		t.Fatal("前置登记学生失败")
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	out, errText, code := runCLI(t, file, "waiver", "s1", "rX", "wbad", invalidUTF8Arg("依据"))
	if code != exitFile {
		t.Fatalf("含非法文字的被拒绝免修应拒绝整次保存、退出码 %d，code=%d out=%q err=%q",
			exitFile, code, out, errText)
	}
	if out != "" || strings.Contains(out, "已拒绝") || strings.Contains(out, "免修历史") {
		t.Fatalf("保存失败时不得宣布申请已拒绝或历史已保存，out=%q", out)
	}
	if !strings.Contains(errText, "免修") || !strings.Contains(errText, "依据") {
		t.Fatalf("错误应指出免修依据无法原样保存，err=%q", errText)
	}

	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("拒绝保存后原文件必须逐字节保留")
	}

	// 重新打开进程：未保存成功的申请不在历史中，原有学生仍可查询。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("原文件应仍可正常查询，code=%d out=%q", code, show)
	}
	if strings.Contains(show, "wbad") {
		t.Fatalf("未保存成功的被拒绝申请不得进入历史，out=%q", show)
	}
}

// TestCLISaveInvalidTextKeepsQueryableState 保存被拒后，原先能够查询的学生与
// 学分仍可正常查询；本次想写入的含非法文字内容不落盘。
func TestCLISaveInvalidTextKeepsQueryableState(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"waiver", "s1", "r1", "w1", "竞赛获奖"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("前置步骤 %v 失败：%q", st, errText)
		}
	}

	// 试图登记名称含非法字节的第二门课程：保存失败。
	assertSaveRejectsBadText(t, file,
		[]string{"course", "c2", invalidUTF8Arg("物理"), "3"}, "课程", "名称", "追加非法课程")

	check, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(check, "总学分：4") ||
		!strings.Contains(check, "来源为有效免修 w1") {
		t.Fatalf("保存被拒后原有学分与免修来源应照常可查，code=%d out=%q", code, check)
	}
	list, _, code := runCLI(t, file, "list-courses")
	if code != 0 || !strings.Contains(list, "数学") || strings.Contains(list, "物理") {
		t.Fatalf("新课程不应落盘、旧课程应仍在，code=%d out=%q", code, list)
	}
}

// TestCLISaveAcceptsLegalUnicodeText 合法文字继续按既有规则保存与使用：
// 用户确实输入的“�”、中文与补充平面字符、编号中合法的控制字符、依据中的
// 空白、命令行字面上的“\uD800”都不能误拒绝，保存后可原样查回。
func TestCLISaveAcceptsLegalUnicodeText(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")

	// 编号中合法的控制字符 U+0001，以及用户确实输入的“�”。
	if out, _, code := runCLI(t, file, "student", "s\x01t"); code != 0 ||
		!strings.Contains(out, "已登记学生") {
		t.Fatalf("含合法控制字符的学生编号应正常登记，code=%d out=%q", code, out)
	}
	if _, _, code := runCLI(t, file, "student", "s�2"); code != 0 {
		t.Fatal("用户确实输入的“�”是合法文字，应正常登记")
	}
	// 中文与补充平面字符课程名称。
	if _, _, code := runCLI(t, file, "course", "c1", "高等数学😀", "4"); code != 0 {
		t.Fatal("含补充平面字符的课程名称应正常登记")
	}
	if _, _, code := runCLI(t, file, "req", "s\x01t", "r1", "c1"); code != 0 {
		t.Fatal("含控制字符编号的学生名下登记要求应正常")
	}
	// 学期含补充平面字符。
	if _, _, code := runCLI(t, file, "enroll", "s\x01t", "r1", "2024😀春", "e1"); code != 0 {
		t.Fatal("含补充平面字符的学期应正常登记")
	}
	// 依据含前后空白与字面上的“\uD800”（反斜线 + 普通字母数字）。
	basis := "  竞赛获奖\t\\uD800  "
	if out, _, code := runCLI(t, file, "waiver", "s\x01t", "r1", "w1", basis); code != 0 ||
		!strings.Contains(out, "有效") {
		t.Fatalf("含空白与字面 \\uD800 的依据应正常生效为有效免修，code=%d out=%q", code, out)
	}

	// 重新打开进程，原样查回。show 用 %q 呈现依据，会把制表符与反斜线转义，
	// 因此期望值也按同样的 %q 规则构造。
	out, _, code := runCLI(t, file, "show", "s\x01t")
	if code != 0 {
		t.Fatalf("含控制字符编号应可查询，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "学生 s\x01t") ||
		!strings.Contains(out, "学期 2024😀春") ||
		!strings.Contains(out, "依据 "+fmt.Sprintf("%q", basis)) {
		t.Fatalf("合法文字应原样保存查回，out=%q", out)
	}
	check, _, code := runCLI(t, file, "check", "s\x01t")
	if code != 0 || !strings.Contains(check, "总学分：4") ||
		!strings.Contains(check, "有效免修 w1") {
		t.Fatalf("正常保存后学分来源与免修历史不应改变，code=%d out=%q", code, check)
	}
}
