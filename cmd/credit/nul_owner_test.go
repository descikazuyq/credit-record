package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件在命令行层回归含空字符 U+0000 编号的要求归属问题。
//
// 空字符编号实际只能经记录文件（JSON 中写作 \u0000）进入系统：
// exec 命令行参数本身不能含空字符，但 run 直接吃字符串参数，因此正向
// 流程在进程内构造；损坏文件场景直接落盘 JSON，与真实文件完全一致。

// nulCLIFile 是含空字符编号场景使用的固定编号（解码后各含一个空字符）。
const (
	clNulStudentA = "s"
	clNulStudentB = "s\x00x"
	clNulReqA     = "x\x00r"
	clNulReqB     = "r"
)

// TestCLINULOwnedRequirementsAndWaivers 两名学生的编号/要求编号在旧式拼接键
// 下相撞：整段正向流程（登记、免修、核对、查看、撤销、重新打开）必须始终
// 按所属学生分清，甲得 4 学分、乙得 3 学分，来源与历史各归各。
func TestCLINULOwnedRequirementsAndWaivers(t *testing.T) {
	file := tempRecordFile(t)
	steps := [][]string{
		{"student", clNulStudentA},
		{"student", clNulStudentB},
		{"course", "c4", "高等数学", "4"},
		{"course", "c3", "线性代数", "3"},
		{"req", clNulStudentA, clNulReqA, "c4"},
		{"req", clNulStudentB, clNulReqB, "c3"},
	}
	for _, st := range steps {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 两人各提交一份指向本人要求的有效免修，编号都叫 wa。
	if out, _, code := runCLI(t, file, "waiver", clNulStudentA, clNulReqA, "wa", "甲的竞赛获奖"); code != 0 ||
		!strings.Contains(out, "免修 wa 有效") {
		t.Fatalf("甲的免修应生效，code=%d out=%q", code, out)
	}
	if out, _, code := runCLI(t, file, "waiver", clNulStudentB, clNulReqB, "wa", "乙的外校修读证明"); code != 0 ||
		!strings.Contains(out, "免修 wa 有效") {
		t.Fatalf("乙的免修应生效，code=%d out=%q", code, out)
	}

	// 核对结果分别为 4 学分和 3 学分，课程、来源对应各自记录。
	out, _, code := runCLI(t, file, "check", clNulStudentA)
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "课程 c4《高等数学》，4 学分") ||
		!strings.Contains(out, "来源为有效免修 wa") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("甲应凭本人免修获得本人课程 4 学分，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", clNulStudentB)
	if code != 0 || !strings.Contains(out, "总学分：3") ||
		!strings.Contains(out, "课程 c3《线性代数》，3 学分") ||
		!strings.Contains(out, "来源为有效免修 wa") {
		t.Fatalf("乙应凭本人免修获得本人课程 3 学分，code=%d out=%q", code, out)
	}

	// 查看历史各保留本人的依据，不能看到对方的申请。
	out, _, _ = runCLI(t, file, "show", clNulStudentA)
	if !strings.Contains(out, `依据 "甲的竞赛获奖"，状态：有效`) ||
		strings.Contains(out, "乙的外校修读证明") {
		t.Fatalf("甲的历史只应有本人的免修依据，out=%q", out)
	}
	out, _, _ = runCLI(t, file, "show", clNulStudentB)
	if !strings.Contains(out, `依据 "乙的外校修读证明"，状态：有效`) ||
		strings.Contains(out, "甲的竞赛获奖") {
		t.Fatalf("乙的历史只应有本人的免修依据，out=%q", out)
	}

	// 撤销甲的免修：甲回到未满足、0 学分；乙不受影响，仍为 3 学分。
	if _, _, code := runCLI(t, file, "revoke-waiver", clNulStudentA, "wa", "材料无法核实"); code != 0 {
		t.Fatal("撤销甲的免修应成功")
	}
	out, _, code = runCLI(t, file, "check", clNulStudentA)
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "已撤销免修：[wa]") {
		t.Fatalf("撤销后甲应 0 学分并保留撤销历史，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", clNulStudentB)
	if code != 0 || !strings.Contains(out, "总学分：3") ||
		strings.Contains(out, "已撤销免修") {
		t.Fatalf("甲的撤销不能影响乙，乙应仍为 3 学分，code=%d out=%q", code, out)
	}

	// 落盘文件保留空字符原文，再次打开后归属与历史依旧。
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `\u0000`) {
		t.Fatalf("编号中的空字符应原样保留在文件中，raw=%q", raw)
	}
	out, _, code = runCLI(t, file, "check", clNulStudentA)
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "已撤销免修：[wa]") {
		t.Fatalf("重开后甲应保持撤销后的核对结果，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", clNulStudentB)
	if code != 0 || !strings.Contains(out, "总学分：3") {
		t.Fatalf("重开后乙应仍为 3 学分，code=%d out=%q", code, out)
	}
}

// TestCLINULWaiverBorrowingOtherStudentRejectsFile 损坏文件：只有乙名下存在
// 要求 "r"，却有一份属于甲、目标为 "x\u0000r" 的有效免修。任何子命令在
// Load 阶段都必须失败（退出码 2）、报告免修目标不属于该学生或不存在、
// 不输出成功核对结果，且原文件字节不变。
func TestCLINULWaiverBorrowingOtherStudentRejectsFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	raw := []byte(`{
  "version": 1,
  "courses": [{"id": "c3", "name": "线性代数", "credit": 3, "open": true}],
  "students": [{"id": "s"}, {"id": "s\u0000x"}],
  "requirements": [{"id": "r", "student": "s\u0000x", "course": "c3"}],
  "waivers": [{"id": "wa", "student": "s", "req": "x\u0000r", "basis": "跨学生借用依据", "status": "approved"}]
}
`)
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	// 核对、查看甚至纯登记命令都必须在读取阶段被挡下，绝不输出成功结果。
	for _, args := range [][]string{
		{"check", clNulStudentA},
		{"show", clNulStudentA},
		{"list-courses"},
		{"student", "anyone"},
	} {
		out, errText, code := runCLI(t, file, args...)
		if code != 2 {
			t.Fatalf("命令 %v 读取损坏文件应返回退出码 2，得到 %d（out=%q err=%q）",
				args, code, out, errText)
		}
		if out != "" {
			t.Fatalf("命令 %v 损坏文件不能输出成功结果，out=%q", args, out)
		}
		if !strings.Contains(errText, "内容损坏") ||
			!strings.Contains(errText, "不属于该学生或不存在") {
			t.Fatalf("命令 %v 应明确报告免修目标不属于该学生或不存在，err=%q", args, errText)
		}
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("损坏文件必须原样保留，不得改动\nwant=%q\n got=%q", raw, got)
	}
}
