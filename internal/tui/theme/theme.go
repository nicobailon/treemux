package theme

import "github.com/charmbracelet/lipgloss"

var (
	BaseBg        = lipgloss.Color("#11111b")
	PanelBg       = lipgloss.Color("#1e1e2e")
	SurfaceBg     = lipgloss.Color("#313244")
	Accent        = lipgloss.Color("#cba6f7")
	Accent2       = lipgloss.Color("#89b4fa")
	Teal          = lipgloss.Color("#94e2d5")
	Peach         = lipgloss.Color("#fab387")
	SuccessColor  = lipgloss.Color("#a6e3a1")
	WarnColor     = lipgloss.Color("#f9e2af")
	ErrorColor    = lipgloss.Color("#f38ba8")
	TextColor     = lipgloss.Color("#cdd6f4")
	SubTextColor  = lipgloss.Color("#a6adc8")
	DimColor      = lipgloss.Color("#6c7086")
	OverlayColor  = lipgloss.Color("#45475a")
	Flamingo      = lipgloss.Color("#f5c2e7")
	Lavender      = lipgloss.Color("#b4befe")
	TrafficRed    = lipgloss.Color("#ff5f56")
	TrafficYellow = lipgloss.Color("#ffbd2e")
	TrafficGreen  = lipgloss.Color("#27c93f")
)

const (
	IconWorktree = ""
	IconCurrent  = ""
	IconOrphan   = ""
	IconCreate   = ""
	IconBranch   = ""
	IconSession  = ""
	IconClean    = ""
	IconPath     = ""
	IconJump     = ""
	IconDelete   = ""
	IconKill     = ""
	IconAdopt    = ""
)

var (
	TitleStyle = lipgloss.NewStyle().
			Foreground(Accent).
			Bold(true)
	SectionStyle = lipgloss.NewStyle().
			Foreground(Accent2).
			Bold(true)
	TextStyle = lipgloss.NewStyle().
			Foreground(TextColor)
	SubTextStyle = lipgloss.NewStyle().
			Foreground(SubTextColor)
	DimStyle = lipgloss.NewStyle().
			Foreground(DimColor)
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ErrorColor).
			Bold(true)
	SuccessStyle = lipgloss.NewStyle().
			Foreground(SuccessColor)
	WarnStyle = lipgloss.NewStyle().
			Foreground(WarnColor)
	ListFrameStyle = lipgloss.NewStyle().
			Padding(1, 2)
	PreviewFrameStyle = lipgloss.NewStyle().
				Padding(1, 2).
				Border(lipgloss.ThickBorder()).
				BorderForeground(Accent)
	ModalStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.DoubleBorder()).
			BorderForeground(Accent)
	KeyStyle = lipgloss.NewStyle().
			Foreground(Teal).
			Bold(true)
	SeparatorStyle = lipgloss.NewStyle().
			Foreground(OverlayColor)
	CurrentStyle = lipgloss.NewStyle().
			Foreground(Accent).
			Bold(true)
	BranchStyle = lipgloss.NewStyle().
			Foreground(SubTextColor)
	SelectedBranchStyle = lipgloss.NewStyle().
				Background(SurfaceBg).
				Foreground(Teal)
	LiveBadgeStyle = lipgloss.NewStyle().
			Background(SuccessColor).
			Foreground(BaseBg).
			Bold(true).
			Padding(0, 1)
)

var (
	PanelBorder         = lipgloss.RoundedBorder()
	TrafficActive       = lipgloss.NewStyle().Foreground(SuccessColor).Render("●●●")
	TrafficInactive     = lipgloss.NewStyle().Foreground(OverlayColor).Render("●●●")
	CachedBranchStyle   = lipgloss.NewStyle().Foreground(Accent2)
	CachedActiveStyle   = lipgloss.NewStyle().Foreground(SuccessColor)
	CachedInactiveStyle = lipgloss.NewStyle().Foreground(OverlayColor)
	CachedNameStyle     = lipgloss.NewStyle().Foreground(TextColor)
	CachedNameSelected  = lipgloss.NewStyle().Foreground(SuccessColor).Bold(true)
	CachedNameMuted     = lipgloss.NewStyle().Foreground(DimColor)
	CachedInactiveText  = lipgloss.NewStyle().Foreground(OverlayColor).Render("○ inactive")
	CachedActionTitle   = lipgloss.NewStyle().Foreground(Teal).Bold(true)
	CachedActionDesc    = lipgloss.NewStyle().Foreground(DimColor)
	CachedActionNormal  = lipgloss.NewStyle().Foreground(TextColor)
	CachedDimStyle      = lipgloss.NewStyle().Foreground(DimColor)
)

var GridLogo = lipgloss.NewStyle().Foreground(SuccessColor).Bold(true).Render("▲ ") +
	lipgloss.NewStyle().Foreground(Flamingo).Bold(true).Render("tree") +
	lipgloss.NewStyle().Foreground(Accent).Bold(true).Render("mu") +
	lipgloss.NewStyle().Foreground(Accent2).Bold(true).Render("x")
