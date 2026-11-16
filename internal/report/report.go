// Пакет report — PDF-отчёт по инцидентам (курсовая).
package report

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jung-kurt/gofpdf/v2"
	"github.com/msdremted-a11y/MiSPISIT/internal/storage"
)

// fontDir — каталог со шрифтом DejaVu (ставится пакетом ttf-dejavu).
// gofpdf трактует путь к файлу относительно SetFontLocation.
func fontDir() string {
	if v := os.Getenv("REPORT_FONT_DIR"); v != "" {
		return v
	}
	return "/usr/share/fonts/dejavu"
}

func statusTitle(s string) string {
	switch s {
	case "open":
		return "открыт"
	case "ack":
		return "подтверждён"
	default:
		return "закрыт"
	}
}

// IncidentsPDF формирует отчёт по инцидентам за период [since, now].
func IncidentsPDF(w io.Writer, product, version string, stats storage.IncidentStats,
	incidents []storage.Incident, since, now time.Time) error {

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetFontLocation(fontDir())
	pdf.AddUTF8Font("dejavu", "", "DejaVuSans.ttf")
	pdf.SetFont("dejavu", "", 12)
	pdf.AddPage()

	pdf.SetFont("dejavu", "", 16)
	pdf.CellFormat(0, 10, product, "", 1, "C", false, 0, "")
	pdf.SetFont("dejavu", "", 12)
	pdf.CellFormat(0, 8, fmt.Sprintf("Отчёт по инцидентам мониторинга (версия %s)", version), "", 1, "C", false, 0, "")
	pdf.Ln(4)

	pdf.CellFormat(0, 7, fmt.Sprintf("Период: %s — %s (UTC)",
		since.UTC().Format("02.01.2006 15:04"), now.UTC().Format("02.01.2006 15:04")), "", 1, "L", false, 0, "")
	pdf.CellFormat(0, 7, fmt.Sprintf("Открыто инцидентов за период: %d, из них не закрыто: %d, закрыто: %d",
		stats.Opened, stats.Open, stats.Closed), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	if len(incidents) == 0 {
		pdf.CellFormat(0, 7, "Инцидентов за период не было.", "", 1, "L", false, 0, "")
		return pdf.Output(w)
	}

	// шапка таблицы
	widths := []float64{12, 60, 32, 32, 26, 28}
	headers := []string{"№", "Правило", "Открыт", "Закрыт", "Статус", "Значение"}
	pdf.SetFillColor(238, 241, 246)
	for i, h := range headers {
		pdf.CellFormat(widths[i], 8, h, "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)
	for _, in := range incidents {
		closed := "—"
		if in.ClosedAt != nil {
			closed = in.ClosedAt.UTC().Format("02.01 15:04")
		}
		row := []string{
			fmt.Sprintf("%d", in.ID), in.RuleName,
			in.OpenedAt.UTC().Format("02.01 15:04"), closed,
			statusTitle(in.Status), fmt.Sprintf("%g", in.LastValue),
		}
		for i, cell := range row {
			pdf.CellFormat(widths[i], 7, cell, "1", 0, "L", false, 0, "")
		}
		pdf.Ln(-1)
	}
	return pdf.Output(w)
}
