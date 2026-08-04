package site6v

// Resource 表示一个搜索命中的资源条目（来自 6v520 列表页）。
type Resource struct {
	Title    string `json:"title"`    // 资源标题
	URL      string `json:"url"`      // 详情页完整 URL
	Date     string `json:"date"`     // 发布日期 YYYY-MM-DD
	Category string `json:"category"` // 分类目录名，如 dy/dlz
}

// Magnet 表示从详情页提取的一条磁力链。
type Magnet struct {
	Name   string `json:"name"`   // 名称（取自链接文本）
	Magnet string `json:"magnet"` // 可用 magnet 链接
	Desc   string `json:"desc"`   // 描述（取自链接文本）
}

// HomeItem 是发现页的一个资源条目。
type HomeItem struct {
	Title    string `json:"title"`         // 资源标题
	URL      string `json:"url"`           // 详情页完整 URL
	Cover    string `json:"cover,omitempty"` // 封面图 URL（列表页爬取时为空）
	Category string `json:"category"`      // 分类目录名，如 dy/dlz
	Date     string `json:"date,omitempty"`  // 发布日期 YYYY-MM-DD
}

// BrowseCategory 是发现页中一个分类及其前 N 条资源。
type BrowseCategory struct {
	Category string     `json:"category"` // 分类目录名，如 dy/dlz
	Name     string     `json:"name"`     // 中文名，如 电影/国剧
	Items    []HomeItem `json:"items"`     // 该分类前 N 条资源
}
