package main

import (
	"fmt"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“课程停开与免修资格的关系”：停开只限制新增修读，
// 既不取消学生名下已有的课程要求，也不禁止凭含实际文字的依据申请免修。
// 每条命令都经 runCLI 重新打开记录文件，申请反馈、学生记录（show）与学分
// 核对（check）之间的断言同时覆盖落盘后再加载的结果。本次只保障现有行为，
// 不增加新的免修条件：
//   - 课程停开、要求没有通过修读也没有有效免修时，用未使用过的免修编号和
//     含实际文字的依据申请应当成功（退出码 0，说明免修有效）；show 能找到
//     属于该学生、指向该要求的申请，编号与依据完整保留；check 把该要求列
//     为已满足、以新免修编号说明来源、计入课程学分并从未满足要求中移除；
//     申请前没有修读记录也允许办理，不能为满足要求自动生成一份通过修读，
//     课程列表仍显示停开；
//   - 停开前已取得通过修读时，同一申请行为仍成功：通过历史不挡住合法免修，
//     申请后仍只计该课程一份学分，当前来源改为有效免修（不能因总学分没有
//     增加而仍显示原来的修读来源），同时保留原通过修读的要求、学期与结果；
//   - 停开不放宽免修原有的拒绝规则：目标要求存在但依据为空字符串或全部由
//     空白字符（普通空格、制表符、换行、全角空格 U+3000 等混用）组成时，
//     退出码 1，申请内容与缺少依据的原因进入该学生的拒绝历史；该要求已有
//     有效免修时再用新编号申请同一要求也退出码 1，原因指出现有有效免修，
//     原申请继续生效。两种拒绝都在 show 与 check 中有据可查，不能误报
//     课程停开，不能清除原历史或改变原有学分来源。

// setupCLIClosedCourseWithReq 登记一门 4 学分课程 c1 与学生 s1 名下指向它
// 的要求 r1，然后把课程停开。调用结束时课程列表显示停开，r1 既无修读也无
// 免修。
func setupCLIClosedCourseWithReq(t *testing.T, file string) {
	t.Helper()
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"course-close", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	if out, _, code := runCLI(t, file, "list-courses"); code != 0 ||
		!strings.Contains(out, "课程 c1《高等数学》4 学分，状态：停开") {
		t.Fatalf("前置条件应为停开课程 c1，code=%d out=%q", code, out)
	}
}

// TestCLIClosedCourseWaiverWithoutEnrollment 课程已停开、要求没有通过修读
// 也没有有效免修：凭未使用过的免修编号与含实际文字的依据申请必须成功，
// 且不能为满足要求凭空生成一份通过修读。
func TestCLIClosedCourseWaiverWithoutEnrollment(t *testing.T) {
	file := tempRecordFile(t)
	setupCLIClosedCourseWithReq(t, file)

	// 申请前该学生没有任何修读记录。
	if out, _, code := runCLI(t, file, "show", "s1"); code != 0 ||
		!strings.Contains(out, "修读：（无）") {
		t.Fatalf("申请前应没有修读记录，code=%d out=%q", code, out)
	}

	// 停开课程上用新免修编号 w1、含实际文字的依据申请：退出码 0 且说明免修有效。
	// 反馈不能把原因说成课程停开，也不能提示任何修读被登记或通过。
	out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "竞赛省级一等奖")
	if code != 0 {
		t.Fatalf("停开课程上凭有效依据申请免修应成功，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(out, "免修 w1 有效") ||
		!strings.Contains(out, "要求 r1") || !strings.Contains(out, "竞赛省级一等奖") {
		t.Fatalf("应说明免修 w1 有效并保留要求与依据，out=%q", out)
	}
	if strings.Contains(errText, "停开") || strings.Contains(out, "修读") {
		t.Fatalf("免修申请不应被停开挡住，也不应提及修读，out=%q err=%q", out, errText)
	}

	// 查看记录：申请属于 s1、指向 r1，编号与依据完整保留、状态有效。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, show)
	}
	if !strings.Contains(show, `免修 w1：要求 r1，依据 "竞赛省级一等奖"，状态：有效`) {
		t.Fatalf("show 应保留免修编号、指向要求与依据原文，out=%q", show)
	}
	// 不能为满足要求自动生成一份修读：修读段落仍为无，不出现任何修读明细行。
	if !strings.Contains(show, "修读：（无）") || strings.Contains(show, "修读 e") {
		t.Fatalf("办理免修不能凭空生成修读记录，out=%q", show)
	}

	// 核对：r1 列为已满足、以新免修编号 w1 说明来源、计入 4 学分，
	// 并从未满足要求中移除；没有修读就不能出现通过修读来源或修读历史。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：已满足，来源为有效免修 w1") ||
		strings.Contains(out, "未满足要求：[") ||
		strings.Contains(out, "来源为通过修读") ||
		strings.Contains(out, "通过修读历史") {
		t.Fatalf("r1 应由免修 w1 满足、计 4 学分且无修读来源，out=%q", out)
	}

	// 课程列表仍须显示停开：免修不改变课程开放状态。
	if out, _, _ := runCLI(t, file, "list-courses"); !strings.Contains(out,
		"课程 c1《高等数学》4 学分，状态：停开") {
		t.Fatalf("办理免修后课程应仍为停开，out=%q", out)
	}

	// 停开仍然只限制新增修读：免修生效后新增修读依旧被拒绝并指出停开，
	// 免修资格与新增修读限制并存。
	_, errText2, code := runCLI(t, file, "enroll", "s1", "r1", "2025春", "e1")
	if code != 1 || !strings.Contains(errText2, "课程 c1 已停开，不能新增修读") {
		t.Fatalf("免修生效后停开仍应禁止新增修读，code=%d err=%q", code, errText2)
	}
	if show, _, _ := runCLI(t, file, "show", "s1"); strings.Contains(show, "修读 e1") {
		t.Fatalf("被拒绝的新增修读不得进入记录，out=%q", show)
	}
}

// TestCLIClosedCourseWaiverAfterPassedEnrollment 停开前已取得的通过结果不
// 挡住合法免修：申请后仍只计一份课程学分，当前来源改为有效免修，原通过
// 修读的要求、学期与通过结果在学生记录中保留。
func TestCLIClosedCourseWaiverAfterPassedEnrollment(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		// 修读在课程开放时登记并提交通过，取得停开前的通过结果。
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
		{"course-close", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 申请前的基线：r1 由停开前的通过修读 e1 满足，总学分 4。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "已满足，来源为通过修读 e1") {
		t.Fatalf("申请前应凭通过修读 e1 计 4 学分，code=%d out=%q", code, out)
	}

	// 停开后用新编号 w2、含实际文字的依据申请：退出码 0，免修有效。
	out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w2", "外校同层次课程成绩单")
	if code != 0 {
		t.Fatalf("已有通过修读不应挡住合法免修申请，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(out, "免修 w2 有效") {
		t.Fatalf("应说明免修 w2 有效，out=%q", out)
	}

	// 核对：总学分仍是 4（只计一份），当前来源改为有效免修 w2，不能因总学分
	// 没有增加而仍显示原来的修读来源；原通过修读作为历史保留、不重复计学分。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") {
		t.Fatalf("通过修读与有效免修并存只计一份 4 学分，out=%q", out)
	}
	if !strings.Contains(out, "已满足，来源为有效免修 w2") ||
		strings.Contains(out, "来源为通过修读") {
		t.Fatalf("当前学分来源应改为有效免修 w2，不能仍显示修读来源，out=%q", out)
	}
	if !strings.Contains(out, "另有通过修读历史 [e1]，不重复计学分") {
		t.Fatalf("应保留原通过修读 e1 的历史并说明不重复计学分，out=%q", out)
	}
	if strings.Contains(out, "未满足要求：[") {
		t.Fatalf("r1 应保持已满足，out=%q", out)
	}

	// 学生记录同时保留：原修读的要求、学期与通过结果，以及状态有效的新免修。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, show)
	}
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：通过") {
		t.Fatalf("应保留原通过修读的要求、学期与通过结果，out=%q", show)
	}
	if !strings.Contains(show, `免修 w2：要求 r1，依据 "外校同层次课程成绩单"，状态：有效`) {
		t.Fatalf("应保留已生效免修 w2 的编号、要求与依据，out=%q", show)
	}

	// 申请状态与核对来源在落盘重新加载后仍准确体现免修已生效：幂等重复提交
	// w2（相同编号相同内容）返回原申请且状态为有效，不退回修读来源。
	if out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w2", "外校同层次课程成绩单"); code != 0 ||
		!strings.Contains(out, "免修 w2 已提交过且内容一致，返回原申请，状态：有效") {
		t.Fatalf("重复提交应幂等返回有效免修 w2，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "来源为有效免修 w2") ||
		strings.Contains(out, "来源为通过修读") {
		t.Fatalf("重复提交后来源仍应是有效免修 w2，code=%d out=%q", code, out)
	}

	// 课程仍显示停开。
	if out, _, _ := runCLI(t, file, "list-courses"); !strings.Contains(out,
		"课程 c1《高等数学》4 学分，状态：停开") {
		t.Fatalf("办理免修后课程应仍为停开，out=%q", out)
	}
}

// TestCLIClosedCourseBlankBasisWaiverRejected 停开不能放宽免修原有的拒绝
// 规则：目标要求存在，但依据为空字符串或全部由空白字符（普通空格、制表符、
// 换行、全角空格 U+3000 等，允许混用）组成时，退出码 1，申请内容与缺少
// 依据的原因进入该学生拒绝历史；不产生有效免修，课程停开不能成为拒绝理由。
func TestCLIClosedCourseBlankBasisWaiverRejected(t *testing.T) {
	blanks := map[string]string{
		"空字符串":        "",
		"普通空格":        "     ",
		"全角空格":        "　　　",
		"制表符与换行":      "\t\n\r",
		"普通空格与全角空格混用": " 　 \t　\n \r　 ",
	}
	for name, basis := range blanks {
		t.Run(name, func(t *testing.T) {
			file := tempRecordFile(t)
			setupCLIClosedCourseWithReq(t, file)

			// 提交纯空白依据：业务拒绝退出码 1。stdout 说明已拒绝并入历史，
			// stderr 的原因是缺少依据，不能误报课程停开。
			out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", basis)
			if code != 1 {
				t.Fatalf("%s：空白依据应退出码 1，code=%d out=%q err=%q", name, code, out, errText)
			}
			if !strings.Contains(out, "免修申请 w1 已拒绝") ||
				!strings.Contains(out, "免修依据为空") {
				t.Fatalf("%s：应说明申请已拒绝且原因为免修依据为空，out=%q", name, out)
			}
			if strings.Contains(errText, "停开") {
				t.Fatalf("%s：拒绝原因不能误报课程停开，err=%q", name, errText)
			}
			if strings.Contains(out, "免修 w1 有效") {
				t.Fatalf("%s：不能产生有效免修，out=%q", name, out)
			}

			// 核对：r1 仍未满足、学分为零；拒绝条目保留编号、指向要求、
			// 依据原文与缺少依据的原因。
			out, _, code = runCLI(t, file, "check", "s1")
			if code != 0 {
				t.Fatalf("%s：核对应成功，code=%d out=%q", name, code, out)
			}
			if !strings.Contains(out, "总学分：0") ||
				!strings.Contains(out, "要求 r1（课程 c1《高等数学》，4 学分）：未满足") ||
				!strings.Contains(out, "未满足要求：[r1]") {
				t.Fatalf("%s：被拒绝的空白依据申请不满足要求、不得学分，out=%q", name, out)
			}
			wantEntry := fmt.Sprintf("免修 w1（要求 r1，依据 %q）：免修依据为空", basis)
			if !strings.Contains(out, wantEntry) {
				t.Fatalf("%s：核对的拒绝历史应保留申请内容与原因\nwant contains %q\nout=%q",
					name, wantEntry, out)
			}
			if strings.Contains(out, "来源为有效免修") {
				t.Fatalf("%s：不能出现有效免修来源，out=%q", name, out)
			}

			// show 同样有据可查：状态已拒绝并带原因。
			show, _, code := runCLI(t, file, "show", "s1")
			if code != 0 {
				t.Fatalf("%s：show 应成功，code=%d out=%q", name, code, show)
			}
			wantShow := fmt.Sprintf("免修 w1：要求 r1，依据 %q，状态：已拒绝（免修依据为空）", basis)
			if !strings.Contains(show, wantShow) {
				t.Fatalf("%s：show 应保留空白依据申请与拒绝原因\nwant contains %q\nout=%q",
					name, wantShow, show)
			}
			// 拒绝不生成修读，课程仍停开。
			if strings.Contains(show, "修读 e") {
				t.Fatalf("%s：拒绝免修不能生成修读记录，out=%q", name, show)
			}
			if out, _, _ := runCLI(t, file, "list-courses"); !strings.Contains(out, "状态：停开") {
				t.Fatalf("%s：课程应仍为停开，out=%q", name, out)
			}
		})
	}
}

// TestCLIClosedCourseDuplicateValidWaiverRejected 停开课程上该要求已有有效
// 免修时，再用新编号申请同一要求仍退出码 1，拒绝原因指出现有有效免修，
// 原申请继续生效、原学分来源不变，两次申请都在 show/check 中有据可查。
func TestCLIClosedCourseDuplicateValidWaiverRejected(t *testing.T) {
	file := tempRecordFile(t)
	setupCLIClosedCourseWithReq(t, file)

	// 第一份免修 w1 正常生效。
	if out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "竞赛省级一等奖"); code != 0 ||
		!strings.Contains(out, "免修 w1 有效") {
		t.Fatalf("首份免修 w1 应生效，code=%d out=%q", code, out)
	}

	// 用未使用过的新编号 w2 就同一要求再次申请：退出码 1，原因指出现有有效
	// 免修 w1；不能误报课程停开。
	out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w2", "又一份依据")
	if code != 1 {
		t.Fatalf("已有有效免修时再次申请应退出码 1，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(out, "免修申请 w2 已拒绝") ||
		!strings.Contains(out, "该要求已有有效免修 w1") {
		t.Fatalf("应说明 w2 已拒绝且原因为已有有效免修 w1，out=%q", out)
	}
	if strings.Contains(errText, "停开") {
		t.Fatalf("拒绝原因不能误报课程停开，err=%q", errText)
	}

	// 核对：总学分仍为 4，r1 来源保持原有效免修 w1（不能改成 w2，也不能退回
	// 修读来源），r1 不在未满足列表；被拒绝的 w2 连同原因出现在拒绝历史中。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "已满足，来源为有效免修 w1") ||
		strings.Contains(out, "来源为有效免修 w2") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("原免修 w1 应继续生效并保持唯一学分来源，out=%q", out)
	}
	if !strings.Contains(out, "被拒绝的免修：") ||
		!strings.Contains(out, `免修 w2（要求 r1，依据 "又一份依据"）：该要求已有有效免修 w1`) {
		t.Fatalf("核对应保留 w2 的申请内容与拒绝原因，out=%q", out)
	}

	// show：w1 仍为有效，w2 为已拒绝并带原因；原历史不被清除。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, show)
	}
	if !strings.Contains(show, `免修 w1：要求 r1，依据 "竞赛省级一等奖"，状态：有效`) {
		t.Fatalf("原免修 w1 应继续有效，out=%q", show)
	}
	if !strings.Contains(show,
		`免修 w2：要求 r1，依据 "又一份依据"，状态：已拒绝（该要求已有有效免修 w1）`) {
		t.Fatalf("w2 应保留为已拒绝并说明现有有效免修 w1，out=%q", show)
	}

	// 原申请 w1 继续生效：相同编号相同内容幂等提交仍返回有效状态；课程停开
	// 与 w2 被拒绝都不改变这一结论。
	if out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "竞赛省级一等奖"); code != 0 ||
		!strings.Contains(out, "状态：有效") {
		t.Fatalf("原免修 w1 幂等查询应仍为有效，code=%d out=%q", code, out)
	}
}

// TestCLIClosedCourseWaiverBasisWithSurroundingWhitespaceAccepted 含实际文字
// 的依据即使前后带空白（普通空格与全角空格混用）仍按原文接受：停开课程上
// 申请成功、状态有效，show 与核对中的依据逐字符保留，不被修剪。
func TestCLIClosedCourseWaiverBasisWithSurroundingWhitespaceAccepted(t *testing.T) {
	file := tempRecordFile(t)
	setupCLIClosedCourseWithReq(t, file)

	basis := " 　竞赛省级一等奖证明　 "
	out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", basis)
	if code != 0 {
		t.Fatalf("含实际文字的依据即使前后带空白也应接受，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(out, "免修 w1 有效") {
		t.Fatalf("免修 w1 应有效，out=%q", out)
	}

	want := fmt.Sprintf(`免修 w1：要求 r1，依据 %q，状态：有效`, basis)
	if show, _, code := runCLI(t, file, "show", "s1"); code != 0 ||
		!strings.Contains(show, want) {
		t.Fatalf("show 应按原文保留前后空白依据\nwant contains %q\nout=%q",
			want, show)
	}
	if out, _, code := runCLI(t, file, "check", "s1"); code != 0 ||
		!strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w1") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("带空白的有效依据应正常满足 r1、计 4 学分，code=%d out=%q", code, out)
	}
}
