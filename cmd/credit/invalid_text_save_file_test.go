package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“保存时拒绝含非法 UTF-8 字节的待保存文字”的命令行行为。
//
// 命令参数在进入登记功能后仍是 Go 字符串，可以携带非法 UTF-8 字节；保存时
// 标准库会把这些字节静默替换成“�”，却照常提示成功。修复后这类保存必须：
//   - 退出码沿用文件错误码 2，标准错误说明保存失败并点名目标记录文件，同时
//     指出无法原样保存的文字的记录类别与具体字段；
//   - 标准输出不得出现登记成功、免修已生效、历史已保存、文件已创建等任何
//     完成提示（业务输出在落盘成功前全部缓存不落屏）；
//   - 目标文件原本不存在时不得留下新记录文件；已存在时逐字节保留，原先能
//     查询的学生与学分仍可正常查询；
//   - 即使问题只出现在一条本应被拒绝（或已撤销）免修的文字里，也按保存
//     失败处理（退出码 2 优先于业务拒绝码 1）。
//
// run/runCLI 在进程内直接调用，参数字符串可以携带任意字节，无需经过操作系统
// 的参数编码。

// badArg 是一个含孤立非法 UTF-8 字节的命令参数。
const badArg = "x\xffy"

// assertInvalidTextSaveFailure 断言一次写入类命令因非法文字在保存阶段失败：
// 退出码 2、标准输出为空，标准错误同时点名目标文件、保存失败、记录类别与
// 具体字段。
func assertInvalidTextSaveFailure(t *testing.T, file string, args []string, category, field, label string) {
	t.Helper()
	out, errText, code := runCLI(t, file, args...)
	if code != exitFile {
		t.Fatalf("%s：含非法文字的保存应退出码 %d，code=%d out=%q err=%q",
			label, exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("%s：保存未完成时标准输出应为空，out=%q", label, out)
	}
	for _, want := range []string{"保存记录文件", file, "失败", "无法原样保存", "不是合法 UTF-8", category, field} {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s：错误输出应包含 %q，err=%q", label, want, errText)
		}
	}
}

// TestCLIInvalidTextNewFileNotCreated 目标文件不存在时，登记学生或课程带上
// 非法 UTF-8 参数都必须保存失败：退出码 2、标准输出为空、不留下记录文件。
func TestCLIInvalidTextNewFileNotCreated(t *testing.T) {
	cases := []struct {
		label, category, field string
		args                   []string
	}{
		{"登记学生编号非法", "学生", "id",
			[]string{"student", "s" + badArg}},
		{"登记课程名称非法", "课程", "name",
			[]string{"course", "c1", "高等" + badArg + "数学", "4"}},
		{"登记课程编号非法", "课程", "id",
			[]string{"course", "c" + badArg, "数学", "4"}},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "records.json")

			assertInvalidTextSaveFailure(t, file, c.args, c.category, c.field, c.label)

			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Fatalf("%s：保存失败后不得创建记录文件，stat err=%v", c.label, err)
			}
			leftover, _ := filepath.Glob(filepath.Join(dir, ".credit-*.tmp"))
			if len(leftover) != 0 {
				t.Fatalf("%s：不得留下临时文件：%v", c.label, leftover)
			}
		})
	}
}

// TestCLIInvalidTextNewRecordInExistingFileKeepsFile 向已有记录追加修读或免修
// 时新文字含非法字节：保存失败、退出码 2、标准输出为空，原文件逐字节保留、
// 仍可正常读取，本次的修读/免修不落盘。
func TestCLIInvalidTextNewRecordInExistingFileKeepsFile(t *testing.T) {
	cases := []struct {
		label, category, field string
		args                   []string
	}{
		{"选课学期非法", "修读", "term",
			[]string{"enroll", "s1", "r1", "2024春" + badArg, "e1"}},
		{"有效免修依据非法", "免修", "basis",
			[]string{"waiver", "s1", "r1", "w1", "竞赛获奖" + badArg}},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "records.json")
			setupCLIForEnrollWaiver(t, file)
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			assertInvalidTextSaveFailure(t, file, c.args, c.category, c.field, c.label)

			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%s：保存失败后原文件应可读：%v", c.label, err)
			}
			if string(got) != string(raw) {
				t.Fatalf("%s：原文件必须逐字节保留\nwant=%q\n got=%q", c.label, raw, got)
			}
			show, _, code := runCLI(t, file, "show", "s1")
			if code != 0 {
				t.Fatalf("%s：原记录应仍可正常查询，code=%d out=%q", c.label, code, show)
			}
			if strings.Contains(show, badArg) {
				t.Fatalf("%s：含非法字节的新记录不应落盘，show=%q", c.label, show)
			}
		})
	}
}

// setupCLIForEnrollWaiver 建立 s1 -> r1 -> c1（4 学分）的基础记录。
func setupCLIForEnrollWaiver(t *testing.T, file string) {
	t.Helper()
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "数学", "4"},
		{"req", "s1", "r1", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("准备步骤 %v 失败：%s", st, errText)
		}
	}
}

// TestCLIInvalidTextRejectedWaiverIsSaveFailure 免修依据含非法字节、且目标要求
// 不存在时，申请本应被业务拒绝并写入免修历史，但历史无法原样保存：必须按保存
// 失败处理（退出码 2 优先于业务拒绝码 1），不能显示“已拒绝/已记入历史”，恢复
// 后历史中也查不到这份申请。
func TestCLIInvalidTextRejectedWaiverIsSaveFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	setupCLIForEnrollWaiver(t, file)

	args := []string{"waiver", "s1", "rX", "wbad", "依据" + badArg}
	out, errText, code := runCLI(t, file, args...)
	if code != exitFile {
		t.Fatalf("含非法文字的被拒绝免修也应保存失败、退出码 %d，code=%d out=%q err=%q",
			exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("保存失败时不得显示已拒绝或已记入历史，out=%q", out)
	}
	for _, want := range []string{"保存记录文件", file, "失败", "免修", "basis"} {
		if !strings.Contains(errText, want) {
			t.Fatalf("错误输出应包含 %q，err=%q", want, errText)
		}
	}

	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 || strings.Contains(show, "wbad") {
		t.Fatalf("未保存成功的申请不得进入免修历史，code=%d out=%q", code, show)
	}
}

// TestCLIInvalidTextKeepsExistingFileAndCredits 修改已有记录时遇非法文字：退出码
// 2、标准输出为空，原文件逐字节保留，原先可查询的学生与 4 学分仍正常查询。
func TestCLIInvalidTextKeepsExistingFileAndCredits(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("准备步骤 %v 失败：%s", st, errText)
		}
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	// 新学生编号含非法字节：保存失败。
	assertInvalidTextSaveFailure(t, file, []string{"student", "s" + badArg}, "学生", "id", "追加非法编号学生")

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("原文件应继续可读：%v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("原文件必须逐字节保留\nwant=%q\n got=%q", raw, got)
	}

	// 原学生与学分照常可查；非法学生不落盘。
	check, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(check, "总学分：4") ||
		!strings.Contains(check, "通过修读 e1") {
		t.Fatalf("原有学分与来源应仍可正常查询，code=%d out=%q", code, check)
	}
	if _, errText, code := runCLI(t, file, "show", "s"+badArg); code == 0 {
		t.Fatalf("非法编号学生不应被保存，err=%q", errText)
	}
}

// TestCLIInvalidTextRevokeReasonSaveFailure 撤销有效免修时撤销原因含非法字节：
// 撤销不能宣布完成，退出码 2、标准输出为空，文件中的免修仍为有效、学分来源
// 不变。
func TestCLIInvalidTextRevokeReasonSaveFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	setupCLIForEnrollWaiver(t, file)
	if _, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "竞赛获奖"); code != 0 {
		t.Fatalf("准备有效免修失败：%s", errText)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	out, errText, code := runCLI(t, file, "revoke-waiver", "s1", "w1", "材料无法核实"+badArg)
	if code != exitFile {
		t.Fatalf("含非法文字的撤销应退出码 %d，code=%d out=%q err=%q",
			exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("保存失败时不得显示已撤销，out=%q", out)
	}
	if !strings.Contains(errText, "reason") || !strings.Contains(errText, "免修") {
		t.Fatalf("错误应点名免修记录的原因字段，err=%q", errText)
	}

	got, err := os.ReadFile(file)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("原文件必须逐字节保留：%v", err)
	}
	check, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(check, "来源为有效免修 w1") {
		t.Fatalf("撤销未保存时免修应仍有效、学分来源不变，code=%d out=%q", code, check)
	}
	show, _, _ := runCLI(t, file, "show", "s1")
	if strings.Contains(show, "状态：已撤销") {
		t.Fatalf("撤销未保存时历史中不应出现已撤销状态，out=%q", show)
	}
}

// TestCLIValidSpecialTextSavesNormally 合法特殊文字经命令行仍正常保存与读取：
// 用户输入的“�”、补充平面字符、编号中的控制字符、依据中的空白，以及字面的
// 反斜线加 uD800（只是普通参数文字）。
func TestCLIValidSpecialTextSavesNormally(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")

	// 含用户输入 U+FFFD 的学生编号，与补充平面字符课程名称。
	if out, _, code := runCLI(t, file, "student", "s�1"); code != 0 {
		t.Fatalf("含 U+FFFD 的编号应正常登记，out=%q code=%d", out, code)
	}
	if _, _, code := runCLI(t, file, "course", "c1", "😀数学", "4"); code != 0 {
		t.Fatal("补充平面字符课程名称应正常登记")
	}
	// 编号中合法的控制字符按原文保留。
	if _, _, code := runCLI(t, file, "student", "s\x01t"); code != 0 {
		t.Fatal("编号中的控制字符应正常登记")
	}
	// 字面文字 \uD800：六个普通 ASCII 字符，不是记录文件里的 Unicode 转义。
	if _, _, code := runCLI(t, file, "student", `\uD800`); code != 0 {
		t.Fatal(`字面 \uD800 只是普通文字，应正常登记`)
	}
	if _, _, code := runCLI(t, file, "req", "s�1", "r1", "c1"); code != 0 {
		t.Fatal("含 U+FFFD 编号学生的要求应正常登记")
	}
	// 有效免修依据前后空白随实际文字一起保留。
	if out, _, code := runCLI(t, file, "waiver", "s�1", "r1", "w1", "  竞赛 获奖 "); code != 0 ||
		!strings.Contains(out, "有效") {
		t.Fatalf("带空白依据的免修应正常生效，code=%d out=%q", code, out)
	}

	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// 字面反斜线在 JSON 中按普通字符转义保存，绝不能成为真正的代理项转义。
	if !strings.Contains(string(raw), `\\uD800`) {
		t.Fatalf("字面 \\uD800 应按普通文字保存，content=%q", raw)
	}

	// 重新打开进程：特殊编号可查、学分来源与免修历史不变。
	check, _, code := runCLI(t, file, "check", "s�1")
	if code != 0 || !strings.Contains(check, "总学分：4") ||
		!strings.Contains(check, "来源为有效免修 w1") {
		t.Fatalf("合法特殊文字保存后学分与来源应正常，code=%d out=%q", code, check)
	}
	show, _, code := runCLI(t, file, "show", "s�1")
	if code != 0 || !strings.Contains(show, `"  竞赛 获奖 "`) {
		t.Fatalf("依据中的原有空白应原样保留，code=%d out=%q", code, show)
	}
	if _, _, code := runCLI(t, file, "show", `\uD800`); code != 0 {
		t.Fatal(`字面 \uD800 编号的学生应能查询`)
	}
	if _, _, code := runCLI(t, file, "show", "s\x01t"); code != 0 {
		t.Fatal("含控制字符编号的学生应能查询")
	}
}
