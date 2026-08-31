package personaldocuments

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/campusos/CampusOS/pkg/apperror"
)

const (
	maxOfficeExpandedBytes = int64(100 * 1024 * 1024)
	maxOfficeEntries       = 10000
	maxOfficeCompression   = uint64(100)
	maxRelationshipBytes   = int64(1024 * 1024)
)

var windowsReservedNames = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {}, "com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {}, "lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

// validateUploadPayload is deliberately bounded before a document enters the
// ObjectPort. A client Content-Type is only a hint and is never persisted as
// the trusted object MIME type.
func (s *Service) validateUploadPayload(name, format string, declaredSize int64, reader io.Reader) ([]byte, string, error) {
	limit := limitFor(format)
	if declaredSize < 0 || declaredSize > limit {
		return nil, "", s.tooLargeError(format, limit, declaredSize)
	}
	if err := validateDocumentName(name, format); err != nil {
		return nil, "", s.invalidUploadError(format, err.Error())
	}
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(raw)) > limit {
		return nil, "", s.tooLargeError(format, limit, int64(len(raw)))
	}
	if int64(len(raw)) != declaredSize {
		return nil, "", s.invalidUploadError(format, "上传流的实际大小与声明大小不一致，请重新选择文件")
	}
	if len(raw) == 0 {
		return nil, "", s.invalidUploadError(format, "不支持上传空文件")
	}

	switch format {
	case FormatText, FormatMarkdown, FormatCampusDoc:
		if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
			return nil, "", s.invalidUploadError(format, "文本文件必须是有效的 UTF-8 文本，且不能包含二进制内容")
		}
		detected := http.DetectContentType(raw)
		if detected == "text/html; charset=utf-8" || strings.HasPrefix(detected, "image/svg+xml") || looksLikeHTMLOrSVG(raw) {
			return nil, "", s.invalidUploadError(format, "文本文件不能伪装为 HTML 或 SVG；请上传纯文本、Markdown 或 CampusDoc")
		}
		if err := validateContent(format, string(raw)); err != nil {
			return nil, "", err
		}
		return raw, "text/plain; charset=utf-8", nil
	case FormatPDF:
		if err := validatePDF(raw); err != nil {
			return nil, "", s.invalidUploadError(format, err.Error())
		}
		return raw, "application/pdf", nil
	case FormatDOCX:
		if err := validateDOCX(raw); err != nil {
			return nil, "", s.invalidUploadError(format, err.Error())
		}
		return raw, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", nil
	default:
		return nil, "", s.invalidUploadError(format, "文件格式不受支持")
	}
}

func (s *Service) tooLargeError(format string, limit, provided int64) error {
	return s.public(ErrInvalid, apperror.PersonalDocumentTooLarge, map[string]any{
		"format": format, "max_file_bytes": limit, "provided_bytes": provided,
		"action": "请压缩文件或拆分内容后重新上传；文件仍会受到个人空间总配额限制。",
	})
}

func (s *Service) invalidUploadError(format, reason string) error {
	return s.public(ErrInvalid, apperror.PersonalDocumentInvalid, map[string]any{
		"field": "file", "format": format, "reason": reason,
		"allowed_formats": []string{FormatText, FormatMarkdown, FormatCampusDoc, FormatPDF, FormatDOCX},
		"action":          "请按提示修正文件后重新上传；不要只修改文件扩展名。",
	})
}

func validateDocumentName(value, format string) error {
	name := strings.TrimSpace(value)
	if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\") || strings.ContainsAny(name, "\x00\r\n") || strings.ContainsAny(name, "\u200b\u200c\u200d\u200e\u200f\u202a\u202b\u202c\u202d\u202e") {
		return errors.New("文件名包含不安全的路径、控制字符或不可见字符")
	}
	base := strings.TrimRight(filepath.Base(name), ". ")
	if base == "" || base != name {
		return errors.New("文件名不能以点或空格结尾")
	}
	parts := strings.Split(base, ".")
	if len(parts) > 2 {
		return errors.New("文件名不能使用双扩展名")
	}
	stem := strings.ToLower(parts[0])
	if _, forbidden := windowsReservedNames[stem]; forbidden {
		return errors.New("文件名不能使用 Windows 保留设备名")
	}
	extension := strings.ToLower(filepath.Ext(base))
	if !nameMatchesFormat(extension, format) {
		return errors.New("文件扩展名与选择的文档格式不一致")
	}
	return nil
}

func nameMatchesFormat(extension, format string) bool {
	switch format {
	case FormatText:
		return extension == ".txt"
	case FormatMarkdown:
		return extension == ".md" || extension == ".markdown"
	case FormatCampusDoc:
		return extension == ".campusdoc" || extension == ".json"
	case FormatPDF:
		return extension == ".pdf"
	case FormatDOCX:
		return extension == ".docx"
	default:
		return false
	}
}

func looksLikeHTMLOrSVG(raw []byte) bool {
	trimmed := strings.ToLower(strings.TrimSpace(string(raw)))
	return strings.HasPrefix(trimmed, "<!doctype html") || strings.HasPrefix(trimmed, "<html") || strings.HasPrefix(trimmed, "<svg")
}

func validatePDF(raw []byte) error {
	if !bytes.HasPrefix(raw, []byte("%PDF-")) || !bytes.Contains(raw, []byte("%%EOF")) {
		return errors.New("PDF 文件签名或结束标记无效，不能只通过修改扩展名上传")
	}
	lower := bytes.ToLower(raw)
	for _, marker := range [][]byte{[]byte("/javascript"), []byte("/js"), []byte("/launch"), []byte("/openaction"), []byte("/aa"), []byte("/embeddedfile"), []byte("/richmedia"), []byte("/encrypt")} {
		if bytes.Contains(lower, marker) {
			return errors.New("PDF 包含脚本、自动动作、嵌入文件或加密等主动内容，当前不允许上传")
		}
	}
	return nil
}

func validateDOCX(raw []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return errors.New("DOCX 必须是有效的 Office Open XML 压缩包")
	}
	if len(reader.File) == 0 || len(reader.File) > maxOfficeEntries {
		return fmt.Errorf("DOCX 压缩包条目数必须在 1 到 %d 之间", maxOfficeEntries)
	}
	var total uint64
	seenContentTypes, seenDocument := false, false
	for _, file := range reader.File {
		if err := validateDOCXEntry(file); err != nil {
			return err
		}
		if file.UncompressedSize64 > uint64(maxOfficeExpandedBytes) || total > uint64(maxOfficeExpandedBytes)-file.UncompressedSize64 {
			return fmt.Errorf("DOCX 解压后的总大小不能超过 %d MiB", maxOfficeExpandedBytes/(1024*1024))
		}
		total += file.UncompressedSize64
		lowerName := strings.ToLower(file.Name)
		if lowerName == "[content_types].xml" {
			seenContentTypes = true
		}
		if lowerName == "word/document.xml" {
			seenDocument = true
		}
		if strings.HasSuffix(lowerName, ".rels") || lowerName == "word/settings.xml" || lowerName == "[content_types].xml" {
			xml, err := inspectDOCXXML(file)
			if err != nil {
				return err
			}
			if lowerName == "[content_types].xml" && !strings.Contains(strings.ToLower(xml), "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml") {
				return errors.New("DOCX 缺少 Word 主文档内容类型，不能上传未知 Office 容器")
			}
		}
	}
	if !seenContentTypes || !seenDocument {
		return errors.New("DOCX 缺少 Office 主文档结构，不能只通过修改扩展名上传")
	}
	return nil
}

func validateDOCXEntry(file *zip.File) error {
	clean := path.Clean(file.Name)
	if file.Name == "" || strings.HasPrefix(file.Name, "/") || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(file.Name, "\\") {
		return errors.New("DOCX 包含不安全的压缩包路径")
	}
	if file.CompressedSize64 == 0 && file.UncompressedSize64 > 0 || file.CompressedSize64 > 0 && file.UncompressedSize64/file.CompressedSize64 > maxOfficeCompression {
		return fmt.Errorf("DOCX 压缩比不能超过 %d:1", maxOfficeCompression)
	}
	lowerName := strings.ToLower(file.Name)
	for _, forbidden := range []string{"vbaproject.bin", "vbadata.xml", "word/embeddings/", ".zip", ".gz", ".7z", ".rar"} {
		if strings.Contains(lowerName, forbidden) {
			return errors.New("DOCX 不能包含宏、嵌入对象或嵌套压缩文件")
		}
	}
	return nil
}

func inspectDOCXXML(file *zip.File) (string, error) {
	if file.UncompressedSize64 > uint64(maxRelationshipBytes) {
		return "", errors.New("DOCX 的关系或配置文件过大")
	}
	r, err := file.Open()
	if err != nil {
		return "", errors.New("DOCX 关系文件无法读取")
	}
	defer r.Close()
	payload, err := io.ReadAll(io.LimitReader(r, maxRelationshipBytes+1))
	if err != nil || int64(len(payload)) > maxRelationshipBytes {
		return "", errors.New("DOCX 关系或配置文件无法安全读取")
	}
	lower := strings.ToLower(string(payload))
	if strings.Contains(lower, "targetmode=\"external\"") || strings.Contains(lower, "targetmode='external'") || strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "attachedtemplate") || strings.Contains(lower, "macroenabled") {
		return "", errors.New("DOCX 不能包含外部关系、远程模板或宏")
	}
	return string(payload), nil
}
