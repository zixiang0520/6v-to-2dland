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
	Title        string `json:"title"`               // 资源标题
	URL          string `json:"url"`                 // 详情页完整 URL
	Cover        string `json:"cover,omitempty"`     // 封面图 URL（列表页爬取时为空）
	Category     string `json:"category"`            // 分类目录名，如 dy/dlz（推送归档用）
	CategoryName string `json:"category_name,omitempty"` // 分类中文名
	Date         string `json:"date,omitempty"`          // 发布日期 YYYY-MM-DD
	Source       string `json:"source,omitempty"`        // 来源：最新电影 / 最新电视剧 / 空=分类列表
}

// HomeCategory 是发现页的一栏（最新电影 / 最新电视剧 / 11 个分类之一）。
type HomeCategory struct {
	Category string     `json:"category"` // 栏 id：gvod-zx / gvod-dsj / dy / dlz …
	Name     string     `json:"name"`     // 中文栏名
	Items    []HomeItem `json:"items"`
}
