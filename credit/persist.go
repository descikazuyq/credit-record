package credit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

	var data fileData
	dec := json.NewDecoder(f)
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

	// 免修（被拒绝的申请允许指向当时不存在的要求）
	approvedReq := map[string]string{} // student+"\x00"+req -> waiverID
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
		if w.Status == WaiverApproved {
			if w.Basis == "" {
				return fmt.Errorf("有效免修 %s 缺少依据", w.ID)
			}
			rk := reqKey(w.StudentID, w.ReqID)
			if s.reqByKey[rk] == nil {
				return fmt.Errorf("有效免修 %s 指向不存在的要求 %s", w.ID, w.ReqID)
			}
			if other, dup := approvedReq[rk]; dup {
				return fmt.Errorf("学生 %s 的要求 %s 同时存在有效免修 %s 与 %s",
					w.StudentID, w.ReqID, other, w.ID)
			}
			approvedReq[rk] = w.ID
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

// Save 将全部记录原子写入 path：先写同目录临时文件，再重命名覆盖，
// 避免写到一半时损坏原文件。
func (s *Store) Save(path string) (err error) {
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
