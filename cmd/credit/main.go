// credit 是本地课程学分与免修的命令行程序。
//
// 用法：credit -f 记录文件 <子命令> [参数...]
//
// 学生与课程：
//
//	student <编号>                              登记学生（编号唯一）
//	course <编号> <名称...> <正整数学分>          登记课程（初始开放）
//	course-close <编号>                          停开课程
//	course-open <编号>                           恢复开放课程
//	list-courses                                列出全部课程及状态
//
// 课程要求（编号在学生名下唯一）：
//
//	req <学生> <要求编号> <课程编号>              为学生登记课程要求
//
// 修读（编号在学生名下唯一）：
//
//	enroll <学生> <要求编号> <学期> <修读编号>    选课登记
//	pass   <学生> <修读编号>                      提交“通过”
//	fail   <学生> <修读编号>                      提交“未通过”
//
// 免修（编号在学生名下唯一）：
//
//	waiver        <学生> <要求编号> <免修编号> <依据...>  提交免修申请
//	revoke-waiver <学生> <免修编号> [原因...]            撤销有效免修
//
// 核对与查询：
//
//	check <学生>                                 按学生核对学分
//	show  <学生>                                 查看学生的要求、修读与免修历史
//
// 记录文件不存在时从空记录开始；文件无法读取或内容损坏（含同一 JSON 对象
// 内字段名称重复）时立即报错，不会把它当作空记录覆盖。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/descikazuyq/credit-record/credit"
)

// 退出码：0 成功；1 业务规则被拒绝；2 记录文件错误。
const (
	exitOK       = 0
	exitRejected = 1
	exitFile     = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("credit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "用法：credit -f 记录文件 <子命令> [参数...]")
		fmt.Fprintln(stderr, "子命令：student course course-close course-open list-courses")
		fmt.Fprintln(stderr, "        req enroll pass fail waiver revoke-waiver check show")
		fmt.Fprintln(stderr, "详情见 credit -h")
	}
	file := fs.String("f", "", "记录文件路径（必需）")
	if err := fs.Parse(args); err != nil {
		return exitRejected
	}
	if strings.TrimSpace(*file) == "" {
		fmt.Fprintln(stderr, "错误：必须用 -f 指定记录文件")
		fs.Usage()
		return exitRejected
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(stderr, "错误：缺少子命令")
		fs.Usage()
		return exitRejected
	}

	store, existed, err := credit.Load(*file)
	if err != nil {
		fmt.Fprintf(stderr, "错误：%v\n", err)
		return exitFile
	}

	cmd, cmdArgs := rest[0], rest[1:]
	code, err := dispatch(store, cmd, cmdArgs, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "错误：%v\n", err)
	}

	// 只有真正发生变更才落盘：只读核对、纯幂等重复、被拒绝而未写入数据的
	// 请求都不改动文件，也不会为一次查询凭空创建空文件。被拒绝的免修申请
	// 已写入免修历史（store 标记为已变更），仍会保存。损坏文件在 Load 阶段
	// 就已返回，绝不会走到这里覆盖原文件。
	if store.Dirty() {
		if saveErr := store.Save(*file); saveErr != nil {
			fmt.Fprintf(stderr, "错误：保存记录文件失败：%v\n", saveErr)
			return exitFile
		}
		if !existed {
			fmt.Fprintf(stdout, "（记录文件 %s 不存在，已从空记录开始并创建）\n", *file)
		}
	}
	return code
}

func dispatch(s *credit.Store, cmd string, args []string, out io.Writer) (int, error) {
	switch cmd {
	case "student":
		return cmdStudent(s, args, out)
	case "course":
		return cmdCourse(s, args, out)
	case "course-close", "course-open":
		return cmdCourseToggle(s, cmd, args, out)
	case "list-courses":
		return cmdListCourses(s, out)
	case "req":
		return cmdReq(s, args, out)
	case "enroll":
		return cmdEnroll(s, args, out)
	case "pass", "fail":
		return cmdResult(s, cmd, args, out)
	case "waiver":
		return cmdWaiver(s, args, out)
	case "revoke-waiver":
		return cmdRevoke(s, args, out)
	case "check":
		return cmdCheck(s, args, out)
	case "show":
		return cmdShow(s, args, out)
	default:
		return exitRejected, fmt.Errorf("未知子命令 %q", cmd)
	}
}

func cmdStudent(s *credit.Store, args []string, out io.Writer) (int, error) {
	if len(args) != 1 {
		return exitRejected, errors.New("用法：student <编号>")
	}
	st, action, err := s.AddStudent(args[0])
	if err != nil {
		return exitRejected, err
	}
	if action == credit.ActionCreated {
		fmt.Fprintf(out, "已登记学生 %s\n", st.ID)
	} else {
		fmt.Fprintf(out, "学生 %s 已存在，返回原记录\n", st.ID)
	}
	return exitOK, nil
}

func cmdCourse(s *credit.Store, args []string, out io.Writer) (int, error) {
	if len(args) < 3 {
		return exitRejected, errors.New("用法：course <编号> <名称...> <正整数学分>")
	}
	id := args[0]
	creditN, err := strconv.Atoi(strings.TrimSpace(args[len(args)-1]))
	if err != nil {
		return exitRejected, fmt.Errorf("学分必须是正整数：%q", args[len(args)-1])
	}
	name := strings.Join(args[1:len(args)-1], " ")
	c, action, err := s.AddCourse(id, name, creditN)
	if err != nil {
		return exitRejected, err
	}
	switch action {
	case credit.ActionCreated:
		fmt.Fprintf(out, "已登记课程 %s《%s》%d 学分，状态：开放\n", c.ID, c.Name, c.Credit)
	case credit.ActionUpdated:
		fmt.Fprintf(out, "课程 %s 尚未被要求引用，已更新为《%s》%d 学分（状态保持：%s）\n",
			c.ID, c.Name, c.Credit, openText(c.Open))
	default:
		fmt.Fprintf(out, "课程 %s 已存在且内容一致，返回原记录（状态保持：%s）\n",
			c.ID, openText(c.Open))
	}
	return exitOK, nil
}

func cmdCourseToggle(s *credit.Store, cmd string, args []string, out io.Writer) (int, error) {
	if len(args) != 1 {
		return exitRejected, fmt.Errorf("用法：%s <编号>", cmd)
	}
	open := cmd == "course-open"
	c, err := s.SetCourseOpen(args[0], open)
	if err != nil {
		return exitRejected, err
	}
	fmt.Fprintf(out, "课程 %s《%s》状态：%s\n", c.ID, c.Name, openText(c.Open))
	return exitOK, nil
}

func cmdListCourses(s *credit.Store, out io.Writer) (int, error) {
	courses := s.Courses()
	if len(courses) == 0 {
		fmt.Fprintln(out, "（尚无课程）")
		return exitOK, nil
	}
	for _, c := range courses {
		fmt.Fprintf(out, "课程 %s《%s》%d 学分，状态：%s\n", c.ID, c.Name, c.Credit, openText(c.Open))
	}
	return exitOK, nil
}

func cmdReq(s *credit.Store, args []string, out io.Writer) (int, error) {
	if len(args) != 3 {
		return exitRejected, errors.New("用法：req <学生> <要求编号> <课程编号>")
	}
	r, action, err := s.AddRequirement(args[0], args[1], args[2])
	if err != nil {
		return exitRejected, err
	}
	if action == credit.ActionCreated {
		fmt.Fprintf(out, "已为学生 %s 登记要求 %s，指向课程 %s\n", r.StudentID, r.ID, r.CourseID)
	} else {
		fmt.Fprintf(out, "学生 %s 的要求 %s 已存在且内容一致，返回原记录\n", r.StudentID, r.ID)
	}
	return exitOK, nil
}

func cmdEnroll(s *credit.Store, args []string, out io.Writer) (int, error) {
	if len(args) != 4 {
		return exitRejected, errors.New("用法：enroll <学生> <要求编号> <学期> <修读编号>")
	}
	e, action, err := s.AddEnrollment(args[0], args[1], args[2], args[3])
	if err != nil {
		return exitRejected, err
	}
	if action == credit.ActionCreated {
		fmt.Fprintf(out, "已登记修读 %s：学生 %s，要求 %s，学期 %s，状态：选课\n",
			e.ID, e.StudentID, e.ReqID, e.Term)
	} else {
		fmt.Fprintf(out, "修读 %s 已存在且内容一致，返回原记录（状态：%s）\n",
			e.ID, zhText(e.Result))
	}
	return exitOK, nil
}

func cmdResult(s *credit.Store, cmd string, args []string, out io.Writer) (int, error) {
	if len(args) != 2 {
		return exitRejected, fmt.Errorf("用法：%s <学生> <修读编号>", cmd)
	}
	result := credit.Passed
	if cmd == "fail" {
		result = credit.Failed
	}
	e, changed, err := s.SubmitResult(args[0], args[1], result)
	if err != nil {
		return exitRejected, err
	}
	if changed {
		fmt.Fprintf(out, "修读 %s 结果已提交：%s\n", e.ID, zhText(e.Result))
	} else {
		fmt.Fprintf(out, "修读 %s 已提交过相同结果“%s”，返回原记录，不重复计学分\n",
			e.ID, zhText(e.Result))
	}
	return exitOK, nil
}

func cmdWaiver(s *credit.Store, args []string, out io.Writer) (int, error) {
	if len(args) < 4 {
		return exitRejected, errors.New("用法：waiver <学生> <要求编号> <免修编号> <依据...>")
	}
	student, reqID, waiverID := args[0], args[1], args[2]
	basis := strings.Join(args[3:], " ")
	w, action, err := s.ApplyWaiver(student, reqID, waiverID, basis)
	if err != nil {
		// 同编号换内容等冲突：不新增记录。
		return exitRejected, err
	}
	if action == credit.ActionExisted {
		fmt.Fprintf(out, "免修 %s 已提交过且内容一致，返回原申请，状态：%s\n",
			w.ID, waiverText(w.Status))
		return exitOK, nil
	}
	if w.Status == credit.WaiverApproved {
		fmt.Fprintf(out, "免修 %s 有效：学生 %s 的要求 %s 凭依据 %q 满足，获得课程学分\n",
			w.ID, w.StudentID, w.ReqID, w.Basis)
		return exitOK, nil
	}
	// 被拒绝：申请内容与原因已记入该学生免修历史。
	fmt.Fprintf(out, "免修申请 %s 已拒绝（已记入学生 %s 的免修历史）：%s\n",
		w.ID, w.StudentID, w.Reason)
	return exitRejected, nil
}

func cmdRevoke(s *credit.Store, args []string, out io.Writer) (int, error) {
	if len(args) < 2 {
		return exitRejected, errors.New("用法：revoke-waiver <学生> <免修编号> [原因...]")
	}
	reason := strings.Join(args[2:], " ")
	w, changed, err := s.RevokeWaiver(args[0], args[1], reason)
	if err != nil {
		return exitRejected, err
	}
	if changed {
		fmt.Fprintf(out, "免修 %s 已撤销，原依据 %q 保留在免修历史；要求按修读情况重新判定\n",
			w.ID, w.Basis)
	} else {
		fmt.Fprintf(out, "免修 %s 已是撤销状态，重复撤销不改变结果（原依据 %q 保留）\n",
			w.ID, w.Basis)
	}
	return exitOK, nil
}

func cmdCheck(s *credit.Store, args []string, out io.Writer) (int, error) {
	if len(args) != 1 {
		return exitRejected, errors.New("用法：check <学生>")
	}
	rep := s.CheckStudent(args[0])
	if !rep.Found {
		// 没有记录的学生：明确的不存在结果，且不写任何数据。
		return exitRejected, fmt.Errorf("%s", rep.String())
	}
	fmt.Fprint(out, rep.String())
	return exitOK, nil
}

func cmdShow(s *credit.Store, args []string, out io.Writer) (int, error) {
	if len(args) != 1 {
		return exitRejected, errors.New("用法：show <学生>")
	}
	student := args[0]
	if s.Student(student) == nil {
		return exitRejected, fmt.Errorf("学生 %s 不存在，没有任何记录", student)
	}
	fmt.Fprintf(out, "学生 %s\n", student)
	reqs := s.Requirements(student)
	if len(reqs) == 0 {
		fmt.Fprintln(out, "课程要求：（无）")
	}
	for _, r := range reqs {
		c := s.Course(r.CourseID)
		fmt.Fprintf(out, "要求 %s -> 课程 %s", r.ID, r.CourseID)
		if c != nil {
			fmt.Fprintf(out, "《%s》%d 学分", c.Name, c.Credit)
		}
		fmt.Fprintln(out)
	}
	enrs := s.Enrollments(student)
	if len(enrs) == 0 {
		fmt.Fprintln(out, "修读：（无）")
	}
	for _, e := range enrs {
		fmt.Fprintf(out, "修读 %s：要求 %s，学期 %s，结果：%s\n",
			e.ID, e.ReqID, e.Term, zhText(e.Result))
	}
	ws := s.Waivers(student)
	if len(ws) == 0 {
		fmt.Fprintln(out, "免修历史：（无）")
	}
	for _, w := range ws {
		line := fmt.Sprintf("免修 %s：要求 %s，依据 %q，状态：%s",
			w.ID, w.ReqID, w.Basis, waiverText(w.Status))
		if w.Reason != "" {
			line += "（" + w.Reason + "）"
		}
		fmt.Fprintln(out, line)
	}
	return exitOK, nil
}

func openText(open bool) string {
	if open {
		return "开放"
	}
	return "停开"
}

func zhText(r credit.Result) string {
	switch r {
	case credit.Passed:
		return "通过"
	case credit.Failed:
		return "未通过"
	default:
		return "选课"
	}
}

func waiverText(st credit.WaiverStatus) string {
	switch st {
	case credit.WaiverApproved:
		return "有效"
	case credit.WaiverRejected:
		return "已拒绝"
	case credit.WaiverRevoked:
		return "已撤销"
	default:
		return string(st)
	}
}
