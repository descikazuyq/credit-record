package credit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// recordVersion 是当前记录文件格式版本。
const recordVersion = 1

// fileData 是记录文件的磁盘格式。
type fileData struct {
	Version       int            `json:"version"`
	Courses       []*Course      `json:"courses"`
	Students      []*Student     `json:"students"`
	Requirements  []*Requirement `json:"requirements"`
	Enrollments   []*Enrollment  `json:"enrollments"`
	Waivers       []*Waiver      `json:"waivers"`
	NextResultSeq int            `json:"nextResultSeq"`
}

// Load 从 path 读取记录。
//
// 文件不存在时返回空记录集，existed=false，且不报错——即“从空记录开始”。
// 文件存在但无法读取（权限、I/O 错误等）或内容损坏（非法 JSON、结构矛盾、
// 引用悬空等）时返回错误，调用方绝不应把该文件当作空记录覆盖写入。
// 空文件同样视为损坏，而不是空记录。
//
// 整份文件必须只含一条完整记录：记录前后允许空白（空格、制表符、换行、
// 回车），但完整记录结束后只要还有任何内容——多出的右花括号或右方括号、
// 拼接的第二段 JSON、普通文字或没写完的 JSON 片段——都判为损坏并拒绝读取，
// 绝不依据已读到的前半条记录继续办理业务。
//
// 同一个 JSON 对象内的字段归属只能确定一次：出现两个完全同名的字段，或
// 两个字段名只有大小写差异却都会被读取功能识别为同一条记录字段（课程的
// credit 与 Credit、修读的 student 与 Student、最外层的 courses 与
// COURSES 等），无论两个值是否相同、顺序是否相邻，整份文件都判为损坏。
// 规则同时适用于最外层对象与其中的每条课程、学生、要求、修读、免修记录：
// 后一份即使是空数组也不能盖掉前一份。字段名以 JSON 解码后的文字比较并
// 按现有解码规则做大小写折叠（直接写出、Unicode 转义、大小写写法语义
// 等同），无法用转义加大小写的组合绕开；限制只针对同一对象，不同记录
// 各自携带同名字段是正常结构，字符串值里提到字段名不算重复。只出现一次
// 的大小写写法仍照常识别（如只有 Credit 的课程按该值读取）；不指向任何
// 记录字段的未知键不在此列，继续按未知字段拒绝。
//
// JSON 字符串的原始内容必须能还原为合法 Unicode 文字：字符串中直接写入的
// 字节不是合法 UTF-8，或 \uXXXX 转义中出现未配对的高位/低位代理项（高位
// 代理项后没有紧跟低位代理项、孤立的低位代理项、两个高位代理项相连等），
// 整份文件都判为内容损坏——标准库会把这些内容静默替换成 U+FFFD，替换后的
// 编号或依据绝不能当成正常内容继续查询、核对或登记，哪怕问题只在一条已拒绝
// 免修的依据里或本次核对不涉及的课程名称里。检查只读不写：不替用户修补转义、
// 不删字符、不改动任何申请状态。合法文字不受影响：中文、直接写入的补充平面
// 字符、完整的高低代理项转义对、转义表达的控制字符，以及用户确实写入的
// “�”（U+FFFD）都按原样使用。
//
// 有效免修的依据校验与首次正常申请一致：任何状态为有效（approved）的免修，
// 其依据必须含有实际文字；依据为空，或全部由空白字符（空格、制表符、换行、
// 回车、全角空格 U+3000、不换行空格 U+00A0 等，允许混用）组成时，整份记录
// 按内容损坏拒绝读取，即使该要求另有通过修读也不例外。已拒绝申请的依据允许
// 为空或只有空白。检查只读不写：不会补填依据、不会改动免修状态，也不会修剪
// 含实际文字的依据中原有空白。
//
// 已拒绝（rejected）免修必须保留含实际文字的拒绝原因：reason 字段没有出现、
// 显式写成 JSON null、空字符串，或只含空白字符（空格、制表符、换行、回车、
// 全角空格 U+3000、不换行空格 U+00A0 等，允许混用），都算没有原因，整份记录
// 按内容损坏拒绝读取。被拒绝的申请必须能说明当时为什么没有生效，缺了原因的
// 文件不能作为正常学分记录继续使用：只要文件中有一份这样的申请就整份拒绝，
// 哪怕它不属于本次查询的学生，或其目标要求已另有通过修读、有效免修、照常能
// 算出学分，也不能跳过这份历史。字段名大小写与 Unicode 转义沿用既有识别规则
// （reason、Reason、REASON 及解码后等同的转义写法都指向同一字段），字段在
// 免修对象中的位置不影响结果。含实际文字的原因按原文保留（包括前后与中间的
// 空白），不要求固定措辞，也不根据当前课程要求重新判断旧申请；因依据为空被
// 拒绝的申请仍允许保留空依据。检查只读不写：不补写推测的原因、不删除申请、
// 不把状态改成有效。有效免修原本可以没有 reason，已撤销免修沿用既有读取与
// 核对行为，二者都不增加原因限制。
//
// 课程开放状态字段显式写成 JSON null（"open":null，open、Open、OPEN 及解码
// 后等同的 Unicode 转义写法都指向同一字段，字段在课程对象中的位置不影响
// 结果）时，整份记录按内容损坏拒绝读取。标准库把 null 解进普通 bool 不报错，
// 字段保持零值 false，未定状态会被读成明确停开，进而误拒新增修读，并在后续
// 保存时把 null 固化成 false；因此只要课程编号、名称、学分合法而开放状态
// 显式为空，就拒绝整份文件——该课程尚未被要求引用、本次只查看另一名学生
// 也不跳过它继续办理。true（开放）与 false（停开）仍是合法状态，停开课程
// 照常可以建立要求、已有修读照常提交成绩；字段完全省略时沿用既有读取规则，
// 本次不改动。课程名称、免修依据中的普通文字“null”（字符串值）不是状态空值；
// 最外层 null、空数组或空列表继续按各自既有规则读取。检查只读不写：不补填
// 状态、不删除课程、不另存部分记录。
func Load(path string) (s *Store, existed bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewStore(), false, nil
		}
		return nil, false, fmt.Errorf("无法读取记录文件 %s：%w", path, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("无法读取记录文件 %s 的信息：%w", path, err)
	}
	if fi.Size() == 0 {
		return nil, true, fmt.Errorf("记录文件 %s 存在但为空，内容已损坏，未做任何修改", path)
	}

	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, false, fmt.Errorf("无法读取记录文件 %s：%w", path, err)
	}

	// 先按原始字节检查 JSON 字符串能否还原为合法 Unicode 文字。标准库解码
	// 会把非法 UTF-8 字节与未配对的高/低位代理项转义静默替换成 U+FFFD，
	// 替换后的学生编号、课程名称或免修依据绝不能当成正常内容继续办理业务
	// （两个本来不同的编号可能被替换成同一个），这样的文件整份判为内容
	// 损坏，哪怕问题只在一条已拒绝免修的依据里或本次核对不涉及的课程名称
	// 里。检查只读不写：不修补转义、不删字符、不改动申请状态。
	if err := scanStringUnicode(raw); err != nil {
		return nil, true, fmt.Errorf(
			"记录文件 %s 内容损坏（包含无效的 Unicode 文字：%v），未做任何修改",
			path, err)
	}

	// 先在 token 流上检查同一 JSON 对象内的字段归属。标准库直接解进结构
	// 体时会用后一个重复键静默覆盖前一个，而且结构体字段按大小写不敏感
	// 匹配 json 标签：同一门课程写了 "credit":4 与 "Credit":9（4 会被
	// 9 悄悄顶替，两个键颠倒又变成 4），最外层同时写 courses 与 COURSES
	// （后一份即使是空数组也会盖掉前一份），修读里的 student 与 Student
	// 都无法明确说明字段归属。这样的记录必须整份判为损坏，绝不能据此
	// 核对或办理——即使冲突只在本次查询没有涉及的课程或免修历史里。
	// 仅扫描第一份完整值，其后内容仍由下方的“完整记录之后不得有多余
	// 内容”检查处理。
	var dupErr *duplicateFieldError
	if err := scanDuplicateKeys(bytes.NewReader(raw)); errors.As(err, &dupErr) {
		if dupErr.first == dupErr.second {
			return nil, true, fmt.Errorf(
				"记录文件 %s 内容损坏（同一 JSON 对象内字段 %q 重复出现，字段归属无法确定），未做任何修改",
				path, dupErr.field)
		}
		return nil, true, fmt.Errorf(
			"记录文件 %s 内容损坏（同一 JSON 对象内字段 %q 与 %q 仅大小写不同，读取时都指向同一记录字段 %q，字段归属无法确定），未做任何修改",
			path, dupErr.first, dupErr.second, dupErr.field)
	}

	// 显式的 "open":null 必须在结构体解码之前拦截。标准库把 JSON null 解进
	// 非指针 bool 时既不报错也不改字段，零值 false 会被原样保留——于是“开放
	// 状态未确定”的课程被读成“停开”：课程列表显示停开、新增修读被误拒，
	// 之后任何一次需要保存的操作还会把未定状态写成明确的 false。因此只要
	// 课程对象显式给出开放状态字段（open/Open/OPEN 及解码后等同的 Unicode
	// 转义写法，位置不限）而值是 null，整份文件判为内容损坏，哪怕该课程
	// 尚未被任何要求引用、本次只查看另一名学生也不跳过它继续办理。扫描只读
	// 不写：不补填状态、不删除课程；字段省略时的既有读取规则（按零值处理）
	// 不在此列，课程名称、免修依据中的普通文字“null”也不是状态空值。
	var openNullErr *nullCourseOpenError
	if err := scanNullCourseOpen(bytes.NewReader(raw)); errors.As(err, &openNullErr) {
		return nil, true, fmt.Errorf(
			"记录文件 %s 内容损坏（%v），未做任何修改", path, err)
	}

	// 已拒绝免修必须保留含实际文字的拒绝原因，同样要在结构体解码之前拦截：
	// 解码后的 Waiver.Reason 是普通 string，字段没有出现、"reason":null 与
	// "reason":"" 都会变成零值空串，无法区分，而这三种（连同只含空白的字符
	// 串）本次都要拒绝。被拒绝的申请必须能说明当时为什么没有生效；缺了原因，
	// check 列出的申请就没有解释，随后登记其他记录还会把这份不完整历史继续
	// 保存。只要文件中有一份这样的已拒绝申请就整份判为内容损坏——哪怕它不
	// 属于本次查询的学生，或其目标要求已另有通过修读、有效免修、照常能算出
	// 学分，也不能跳过这份历史继续办理。扫描只读不写：不补写推测的原因、不
	// 删除申请、不把状态改成有效。有效免修原本可以没有 reason，已撤销免修
	// 沿用既有读取与核对行为，两者都不受此限制。
	var missingReasonErr *missingRejectReasonError
	if err := scanRejectedWaiverReasons(bytes.NewReader(raw)); errors.As(err, &missingReasonErr) {
		return nil, true, fmt.Errorf(
			"记录文件 %s 内容损坏（%v），未做任何修改", path, err)
	}

	var data fileData
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		return nil, true, fmt.Errorf("记录文件 %s 内容损坏（JSON 解析失败：%v），未做任何修改", path, err)
	}
	// 完整记录之后必须只剩下空白：再解一条原始 JSON，仅 io.EOF 表示记录后
	// 别无他物。不能用 dec.More() 判断——它只向前看“值起始”标记，记录后
	// 多出的右花括号/右方括号会让 More() 直接返回 false 而被漏掉。再次
	// Decode 能覆盖全部情形：成功读到值说明后面拼接了第二段 JSON；返回
	// 语法错误或 unexpected EOF 说明尾部有孤立括号、普通文字或未写完的
	// 片段。这些一律视为文件损坏。
	var extra json.RawMessage
	switch err := dec.Decode(&extra); {
	case errors.Is(err, io.EOF):
		// 唯一一份完整记录之后只有空白，符合格式。
	case err == nil:
		return nil, true, fmt.Errorf("记录文件 %s 内容损坏（完整记录之后还拼接了其他 JSON 数据），未做任何修改", path)
	default:
		return nil, true, fmt.Errorf("记录文件 %s 内容损坏（完整记录之后仍有无法解析的多余内容），未做任何修改", path)
	}

	s = NewStore()
	if err := s.loadData(&data); err != nil {
		return nil, true, fmt.Errorf("记录文件 %s 内容损坏（%v），未做任何修改", path, err)
	}
	return s, true, nil
}

// loadData 校验磁盘数据并重建索引；任何矛盾都作为损坏报错。
func (s *Store) loadData(d *fileData) error {
	if d.Version != recordVersion {
		return fmt.Errorf("不支持的记录版本 %d（当前支持版本 %d）", d.Version, recordVersion)
	}

	// 课程
	for _, c := range d.Courses {
		if c == nil {
			return errors.New("存在空的课程记录")
		}
		if c.ID == "" {
			return errors.New("存在没有编号的课程")
		}
		if c.Name == "" {
			return fmt.Errorf("课程 %s 没有名称", c.ID)
		}
		if c.Credit <= 0 {
			return fmt.Errorf("课程 %s 学分不是正整数：%d", c.ID, c.Credit)
		}
		if _, dup := s.courseByID[c.ID]; dup {
			return fmt.Errorf("课程编号 %s 重复", c.ID)
		}
		s.courses = append(s.courses, c)
		s.courseByID[c.ID] = c
	}

	// 学生
	for _, st := range d.Students {
		if st == nil || st.ID == "" {
			return errors.New("存在没有编号的学生记录")
		}
		if _, dup := s.studentByID[st.ID]; dup {
			return fmt.Errorf("学生编号 %s 重复", st.ID)
		}
		s.students = append(s.students, st)
		s.studentByID[st.ID] = st
	}

	// 要求
	for _, r := range d.Requirements {
		if r == nil {
			return errors.New("存在空的课程要求记录")
		}
		if r.StudentID == "" || r.ID == "" {
			return errors.New("存在缺少学生编号或要求编号的课程要求")
		}
		if _, ok := s.studentByID[r.StudentID]; !ok {
			return fmt.Errorf("要求 %s 引用了不存在的学生 %s", r.ID, r.StudentID)
		}
		if _, ok := s.courseByID[r.CourseID]; !ok {
			return fmt.Errorf("学生 %s 的要求 %s 引用了不存在的课程 %s",
				r.StudentID, r.ID, r.CourseID)
		}
		key := reqKey(r.StudentID, r.ID)
		if _, dup := s.reqByKey[key]; dup {
			return fmt.Errorf("学生 %s 的要求编号 %s 重复", r.StudentID, r.ID)
		}
		dk := dupKey(r.StudentID, r.CourseID)
		if other, dup := s.reqByDup[dk]; dup {
			return fmt.Errorf("学生 %s 就课程 %s 存在重复要求 %s 与 %s",
				r.StudentID, r.CourseID, other, r.ID)
		}
		s.requirements = append(s.requirements, r)
		s.reqByKey[key] = r
		s.reqByDup[dk] = r.ID
	}

	// 修读
	usedSeq := map[int]bool{}
	for _, e := range d.Enrollments {
		if e == nil {
			return errors.New("存在空的修读记录")
		}
		if e.StudentID == "" || e.ID == "" {
			return errors.New("存在缺少学生编号或修读编号的修读记录")
		}
		if _, ok := s.studentByID[e.StudentID]; !ok {
			return fmt.Errorf("修读 %s 引用了不存在的学生 %s", e.ID, e.StudentID)
		}
		r := s.reqByKey[reqKey(e.StudentID, e.ReqID)]
		if r == nil {
			return fmt.Errorf("学生 %s 的修读 %s 引用了不存在的要求 %s",
				e.StudentID, e.ID, e.ReqID)
		}
		if e.Term == "" {
			return fmt.Errorf("学生 %s 的修读 %s 缺少学期", e.StudentID, e.ID)
		}
		switch e.Result {
		case Enrolled, Passed, Failed:
		default:
			return fmt.Errorf("学生 %s 的修读 %s 含非法结果 %q", e.StudentID, e.ID, e.Result)
		}
		if e.Result == Enrolled {
			if e.ResultSeq != 0 {
				return fmt.Errorf("修读 %s 尚未提交结果却带有结果序号", e.ID)
			}
		} else {
			if e.ResultSeq <= 0 {
				return fmt.Errorf("修读 %s 已提交结果但缺少结果序号", e.ID)
			}
			if usedSeq[e.ResultSeq] {
				return fmt.Errorf("结果提交序号 %d 重复，记录已损坏", e.ResultSeq)
			}
			usedSeq[e.ResultSeq] = true
		}
		key := reqKey(e.StudentID, e.ID)
		if _, dup := s.enrByKey[key]; dup {
			return fmt.Errorf("学生 %s 的修读编号 %s 重复", e.StudentID, e.ID)
		}
		s.enrollments = append(s.enrollments, e)
		s.enrByKey[key] = e
	}

	// 免修（被拒绝的申请允许指向当时不存在的要求、依据也可以为空；
	// 已撤销的申请则必须保留一份曾经有效的申请所必需的信息）
	approvedReq := map[ownerKey]string{} // (student, req) -> waiverID
	for _, w := range d.Waivers {
		if w == nil {
			return errors.New("存在空的免修记录")
		}
		if w.StudentID == "" || w.ID == "" {
			return errors.New("存在缺少学生编号或免修编号的免修记录")
		}
		if _, ok := s.studentByID[w.StudentID]; !ok {
			return fmt.Errorf("免修 %s 引用了不存在的学生 %s", w.ID, w.StudentID)
		}
		switch w.Status {
		case WaiverApproved, WaiverRejected, WaiverRevoked:
		default:
			return fmt.Errorf("学生 %s 的免修 %s 含非法状态 %q", w.StudentID, w.ID, w.Status)
		}
		switch w.Status {
		case WaiverApproved:
			// 有效免修的依据校验与首次正常申请完全一致：依据必须含有实际
			// 文字，空串或全部由空白字符（空格、制表符、换行、全角空格
			// U+3000、不换行空格 U+00A0 等，含混用）组成都按内容损坏
			// 拒绝整份记录——这样的“有效免修”无法有据可查，即使该要求
			// 另有通过修读也不能放过。检查只读不写，绝不修剪或改写文件
			// 中已保存的依据原文，也不把它降级成已拒绝/已撤销。
			if blankBasis(w.Basis) {
				return fmt.Errorf("学生 %s 的有效免修 %s 缺少依据（依据为空或全部为空白字符）",
					w.StudentID, w.ID)
			}
			rk := reqKey(w.StudentID, w.ReqID)
			if s.reqByKey[rk] == nil {
				return fmt.Errorf("有效免修 %s 指向的要求 %s 不存在或不属于该学生", w.ID, w.ReqID)
			}
			if other, dup := approvedReq[rk]; dup {
				return fmt.Errorf("学生 %s 的要求 %s 同时存在有效免修 %s 与 %s",
					w.StudentID, w.ReqID, other, w.ID)
			}
			approvedReq[rk] = w.ID
		case WaiverRevoked:
			// 撤销只取消免修对课程要求的满足作用，不能把缺少原依据、
			// 失去要求归属的记录变成合法历史：目标要求必须真实存在于
			// 该免修所属学生名下（同号要求只在其他学生名下不算），
			// 原依据必须含有非空白内容（空白口径与有效免修一致，全角
			// 空格、不换行空格等同样不算实际依据）。检查只读不写，
			// 依据中有实际文字时原有空白一律保留，绝不为了通过检查
			// 改写保存下来的材料内容。失效历史不参与有效免修的唯一
			// 性限制。
			if s.reqByKey[reqKey(w.StudentID, w.ReqID)] == nil {
				return fmt.Errorf("学生 %s 的已撤销免修 %s 目标要求无效（要求 %s 不存在于该学生名下）",
					w.StudentID, w.ID, w.ReqID)
			}
			if blankBasis(w.Basis) {
				return fmt.Errorf("学生 %s 的已撤销免修 %s 原依据为空", w.StudentID, w.ID)
			}
		case WaiverRejected:
			// 已拒绝申请必须保留含实际文字的拒绝原因（字段缺失、null、空串
			// 或全部为空白字符都算没有原因）。正常读取时这一点已由
			// scanRejectedWaiverReasons 在结构体解码之前保证（缺失与 null 经
			// 解码都会变成空串，无法再区分，所以必须在扫描阶段拦截）；这里
			// 再兜底一次，使“拒绝历史必有原因”成为 loadData 自身的不变量，
			// 直接构造 fileData 调用本函数也绕不过去。检查只读不写：不补写
			// 推测的原因、不删除申请、不把状态改成有效。
			if strings.TrimSpace(w.Reason) == "" {
				return fmt.Errorf("学生 %s 的已拒绝免修 %s 缺少拒绝原因（原因为空或全部为空白字符）",
					w.StudentID, w.ID)
			}
		}
		key := reqKey(w.StudentID, w.ID)
		if _, dup := s.waiverByKey[key]; dup {
			return fmt.Errorf("学生 %s 的免修编号 %s 重复", w.StudentID, w.ID)
		}
		s.waivers = append(s.waivers, w)
		s.waiverByKey[key] = w
	}

	if d.NextResultSeq < 0 {
		return errors.New("结果提交序号计数器为负数")
	}
	for seq := range usedSeq {
		if seq > d.NextResultSeq {
			return fmt.Errorf("结果提交序号计数器 %d 小于已有序号 %d", d.NextResultSeq, seq)
		}
	}
	s.nextResultSeq = d.NextResultSeq
	return nil
}

// invalidTextError 表示待保存记录中含有无法原样写入文件的文字：某个字符串
// 字段不是合法 UTF-8。category 指出记录类别（课程、学生、课程要求、修读、
// 免修），id 是该条记录的编号（仅用于定位，编号本身也可能就是问题所在，
// 按原始字节呈现），field 指出具体字段。
type invalidTextError struct {
	category string
	id       string
	field    string
}

func (e *invalidTextError) Error() string {
	return fmt.Sprintf(
		"%s记录（编号 %s）的字段“%s”含无法原样保存的非法 UTF-8 文字",
		e.category, e.id, e.field)
}

// validateSaveText 检查全部待写入记录的字符串字段是否都是合法 UTF-8。
//
// 背景：encoding/json 在序列化时会把字符串里的非法 UTF-8 字节静默替换成
// U+FFFD（“�”）却照常返回成功。若直接保存，免修依据会被改写，两个本来
// 不同的学生编号还可能被替换成同一个编号，让原本可读的记录文件变成编号
// 重复的损坏文件，而命令却提示成功。因此凡是要写入文件的文字——课程、
// 学生、课程要求、修读、免修的编号、名称、学期、依据、原因等——都必须先
// 通过本检查，任何一个字段不合法就拒绝整次保存。
//
// 检查只读不补：绝不替换、清空或修补任何原始字符串，也不删除出问题的
// 那条历史（即使它只是一条已拒绝或已撤销的免修），更不会跳过该字段只存
// 其他记录。合法文字不受影响：中文、补充平面字符、用户确实输入的“�”
// （U+FFFD）都是合法 UTF-8；编号中合法的控制字符与免修依据中原有的空白
// 也原样保留。命令行里字面出现的“\uD800”只是反斜线加普通字母数字，是
// 合法 ASCII 文字，不按记录文件中的 Unicode 转义处理。
func (s *Store) validateSaveText() error {
	for _, c := range s.courses {
		if c == nil {
			continue
		}
		if err := checkSaveText("课程", c.ID, "编号", c.ID); err != nil {
			return err
		}
		if err := checkSaveText("课程", c.ID, "名称", c.Name); err != nil {
			return err
		}
	}
	for _, st := range s.students {
		if st == nil {
			continue
		}
		if err := checkSaveText("学生", st.ID, "编号", st.ID); err != nil {
			return err
		}
	}
	for _, r := range s.requirements {
		if r == nil {
			continue
		}
		if err := checkSaveText("课程要求", r.ID, "编号", r.ID); err != nil {
			return err
		}
		if err := checkSaveText("课程要求", r.ID, "学生编号", r.StudentID); err != nil {
			return err
		}
		if err := checkSaveText("课程要求", r.ID, "课程编号", r.CourseID); err != nil {
			return err
		}
	}
	for _, e := range s.enrollments {
		if e == nil {
			continue
		}
		if err := checkSaveText("修读", e.ID, "编号", e.ID); err != nil {
			return err
		}
		if err := checkSaveText("修读", e.ID, "学生编号", e.StudentID); err != nil {
			return err
		}
		if err := checkSaveText("修读", e.ID, "要求编号", e.ReqID); err != nil {
			return err
		}
		if err := checkSaveText("修读", e.ID, "学期", e.Term); err != nil {
			return err
		}
		if err := checkSaveText("修读", e.ID, "结果", string(e.Result)); err != nil {
			return err
		}
	}
	for _, w := range s.waivers {
		if w == nil {
			continue
		}
		if err := checkSaveText("免修", w.ID, "编号", w.ID); err != nil {
			return err
		}
		if err := checkSaveText("免修", w.ID, "学生编号", w.StudentID); err != nil {
			return err
		}
		if err := checkSaveText("免修", w.ID, "要求编号", w.ReqID); err != nil {
			return err
		}
		if err := checkSaveText("免修", w.ID, "依据", w.Basis); err != nil {
			return err
		}
		if err := checkSaveText("免修", w.ID, "状态", string(w.Status)); err != nil {
			return err
		}
		if err := checkSaveText("免修", w.ID, "原因", w.Reason); err != nil {
			return err
		}
	}
	return nil
}

// duplicateResultSeqError 表示待保存记录中有两份不同修读的已提交成绩共用同一个
// 正整数结果提交顺序编号。seq 是冲突的编号；first/second 各用完整学生编号与
// 修读编号共同指明冲突双方，便于调用方分辨不同学生名下编号相同的修读（例如
// 两名学生各有一份 e1）。顺序按修读在记录中的先后确定，first 先出现。
type duplicateResultSeqError struct {
	seq           int
	firstStudent  string
	firstEnr      string
	secondStudent string
	secondEnr     string
}

func (e *duplicateResultSeqError) Error() string {
	return fmt.Sprintf(
		"成绩提交顺序编号 %d 被两份不同修读的已提交成绩共用：学生 %s 的修读 %s 与学生 %s 的修读 %s",
		e.seq, e.firstStudent, e.firstEnr, e.secondStudent, e.secondEnr)
}

// validateResultSeqUniqueness 检查全部已提交成绩的结果提交顺序编号互不相同。
//
// 顺序编号刻画的是整份记录中每次“首次提交成绩”的先后，通过与未通过都占用
// 一次编号，它不按学生、课程要求或学期各自编号：同一学生的重复修读、不同
// 要求下的独立提交、不同学生的独立修读，只要两份不同修读的已提交成绩共用
// 同一个正整数编号，写出的文件随后就会被 Load 判为内容损坏。因此这样的
// 待保存记录必须在序列化与临时文件之前整次拒绝保存——绝不删修读、不替
// 调用方重新编号、不清成绩或免修历史，也不能用这份记录替换掉原本可用的
// 记录文件。
//
// 尚未提交结果、仍处于选课状态的修读沿用编号 0，多份并存不构成重复，本
// 检查直接跳过它们；判定只看已提交成绩携带的正整数编号，因此已提交编号有
// 空缺（不连续）合法，冲突记录之间夹着其他正常成绩或免修也不能掩盖冲突。
// 以修读在记录中的先后取最先相撞的一对，结果确定且与 map 遍历顺序无关。
func (s *Store) validateResultSeqUniqueness() error {
	first := map[int]*Enrollment{}
	for _, e := range s.enrollments {
		if e == nil || e.Result == Enrolled {
			// 选课记录的编号一律是 0，多份并存正常，不参与重复判定。
			continue
		}
		if e.ResultSeq <= 0 {
			// 本规则只管“正整数顺序编号被共用”：非正编号不是这里的判定对象。
			continue
		}
		if prev := first[e.ResultSeq]; prev != nil {
			return &duplicateResultSeqError{
				seq:           e.ResultSeq,
				firstStudent:  prev.StudentID,
				firstEnr:     prev.ID,
				secondStudent: e.StudentID,
				secondEnr:     e.ID,
			}
		}
		first[e.ResultSeq] = e
	}
	return nil
}

// duplicateApprovedWaiverError 表示待保存记录中同一学生名下的同一项课程要求
// 同时有两份不同编号、状态为有效（approved）的免修。student 是所属学生编号，
// req 是要求编号，first/second 是冲突的两份免修编号（按免修在记录中的先后，
// first 先出现），便于调用方定位具体申请。
type duplicateApprovedWaiverError struct {
	student string
	req     string
	first   string
	second  string
}

func (e *duplicateApprovedWaiverError) Error() string {
	return fmt.Sprintf(
		"学生 %s 的要求 %s 同时存在两份有效免修 %s 与 %s，同一要求只能由一份有效免修取代",
		e.student, e.req, e.first, e.second)
}

// validateApprovedWaiverUniqueness 检查同一学生名下的同一项要求至多有一份有效
// 免修。正常申请流程会拒绝为已有有效免修的要求再生效第二份，读取记录文件时
// （loadData）也拒绝这类冲突；但调用方取得免修记录后可以直接修改其内容——把
// 另一份申请改成有效，或把一份有效申请的目标改到已由免修满足的要求——这样
// 写出的文件随后必然被 Load 判为内容损坏。因此这样的待保存记录必须在序列化
// 与临时文件之前整次拒绝保存：绝不删除申请、不替调用方选定一份有效免修，也
// 不改状态、目标要求或依据，更不能用这份记录替换掉原本可用的记录文件。
//
// 冲突只按（所属学生、目标要求）的完整编号判定，沿用 ownerKey 的既有匹配规则：
// 编号前后的空白也是编号内容，“ r1 ”与“r1 ”是两项不同要求，各自的有效免修
// 互不冲突。两份申请依据相同也不是同一份免修，仍按编号区分；该要求另有通过
// 修读、已经能获得学分，或冲突申请之间夹着其他合法历史，都不能掩盖冲突。
// 已拒绝、已撤销的申请不占用有效名额，与一份有效免修共存时照常保存。
// 以免修在记录中的先后取最先相撞的一对，结果确定且与 map 遍历顺序无关。
func (s *Store) validateApprovedWaiverUniqueness() error {
	approved := map[ownerKey]string{} // (student, req) -> waiverID
	for _, w := range s.waivers {
		if w == nil || w.Status != WaiverApproved {
			// 失效申请（已拒绝/已撤销）不满足任何要求，不参与有效名额判定。
			continue
		}
		rk := reqKey(w.StudentID, w.ReqID)
		if other, dup := approved[rk]; dup {
			return &duplicateApprovedWaiverError{
				student: w.StudentID,
				req:     w.ReqID,
				first:   other,
				second:  w.ID,
			}
		}
		approved[rk] = w.ID
	}
	return nil
}

// checkSaveText 校验单个待保存字段的文字是否为合法 UTF-8。
func checkSaveText(category, id, field, value string) error {
	if !utf8.ValidString(value) {
		return &invalidTextError{category: category, id: id, field: field}
	}
	return nil
}

// Save 将全部记录原子写入 path：先写同目录临时文件，再重命名覆盖，
// 避免写到一半时损坏原文件。
//
// 写入前先做三项检查：
//
// 一是全部待保存字符串都必须是合法 UTF-8：标准库序列化会把非法字节静默
// 替换成 U+FFFD，因此含非法文字的数据绝不能进入序列化与临时文件阶段。
// 校验失败时整次保存直接返回错误——不创建/不改动目标文件（目标原本不
// 存在时也不会留下新记录文件或临时文件），不替换、清空或修补待保存记录
// 中的任何原始字符串，即使问题只出现在一条已拒绝或已撤销免修的字段里。
//
// 二是全部已提交成绩的结果提交顺序编号互不相同：顺序编号表示整份记录中
// 每次首次提交成绩的先后，通过与未通过都占用一次编号，不按学生、要求或
// 学期各自编号。两份不同修读（同一学生的重复修读或不同学生的独立修读，
// 无论修读编号、要求编号、学期是否相同，也无论两条记录是否相邻）的已提交
// 成绩共用同一个正整数编号时，写出的文件随后必然被 Load 判为内容损坏，
// 因此直接拒绝整次保存：错误点名目标文件、重复编号及冲突双方（完整学生
// 编号 + 修读编号），已有文件全部内容原样保留，目标原本不存在时不创建
// 记录文件也不遗留临时文件；待保存的内存记录同样原样保留——不删修读、
// 不替调用方调整顺序、不清成绩或免修历史。仍是选课、尚未提交结果的修读
// 沿用编号 0，多份并存不构成重复；已提交编号有空缺（不连续）仍合法。
//
// 三是同一学生名下的同一项要求至多有一份有效免修：正常申请流程与 Load 都
// 拒绝同一要求同时存在两份有效免修，但调用方取得免修记录后可以直接改内容
// （把另一份申请改成有效，或把有效申请的目标改到已由免修满足的要求），
// 这样写出的文件随后必然被 Load 判为内容损坏。因此直接拒绝整次保存：错误
// 点名目标文件、所属学生、要求编号与冲突的两份免修编号，已有文件全部内容
// 原样保留，目标原本不存在时不创建记录文件也不遗留临时文件；待保存的内存
// 记录同样原样保留——不删除申请、不替调用方选定一份有效免修，也不改状态、
// 目标要求或依据。冲突只按（所属学生、目标要求）的完整编号判定，编号前后
// 的空白也是编号内容；两份申请依据相同仍算两份；该要求另有通过修读、冲突
// 申请之间夹着其他合法历史都不能掩盖冲突。不同学生各自拥有同号要求、同号
// 免修是合法的；同一学生的不同要求各有一份有效免修也正常保存；已拒绝、
// 已撤销的历史不占用有效名额，与一份有效免修共存时照常保存。
func (s *Store) Save(path string) (err error) {
	if err := s.validateSaveText(); err != nil {
		return fmt.Errorf("记录中存在无法原样保存的文字，拒绝写入 %s：%w", path, err)
	}
	if err := s.validateResultSeqUniqueness(); err != nil {
		return fmt.Errorf("记录中的成绩提交顺序编号冲突，拒绝写入 %s，已有文件保持原样：%w", path, err)
	}
	if err := s.validateApprovedWaiverUniqueness(); err != nil {
		return fmt.Errorf("记录中存在有效免修冲突，拒绝写入 %s，已有文件保持原样：%w", path, err)
	}
	data := fileData{
		Version:       recordVersion,
		Courses:       s.courses,
		Students:      s.students,
		Requirements:  s.requirements,
		Enrollments:   s.enrollments,
		Waivers:       s.waivers,
		NextResultSeq: s.nextResultSeq,
	}
	buf, err := json.MarshalIndent(&data, "", "  ")
	if err != nil {
		return fmt.Errorf("无法序列化记录：%w", err)
	}
	buf = append(buf, '\n')

	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, ".credit-*.tmp")
	if err != nil {
		return fmt.Errorf("无法在 %s 创建临时记录文件：%w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(buf); err != nil {
		return fmt.Errorf("写入记录文件失败：%w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("写入记录文件失败：%w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("写入记录文件失败：%w", err)
	}
	if err = os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("设置记录文件权限失败：%w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("保存记录文件 %s 失败：%w", path, err)
	}
	return nil
}
