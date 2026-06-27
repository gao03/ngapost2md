package config

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/crypto/pbkdf2"
	"gopkg.in/ini.v1"
)

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

type NetworkConfig struct {
	BaseURL      string
	UserAgent    string
	Cookie       string
	CookieSource string
}

type chromeCookieDB struct {
	path            string
	profile         string
	keychainService string
}

// ResolveNetworkConfig 返回运行时网络配置。显式 Cookie 优先，未配置时尝试从 Chrome 读取。
func ResolveNetworkConfig(cfg *ini.File) (NetworkConfig, error) {
	baseURL := cfg.Section("network").Key("base_url").MustString("https://bbs.nga.cn")
	ua := cfg.Section("network").Key("ua").String()
	if isEmptyOrPlaceholder(ua) {
		ua = defaultUserAgent
	}

	uid := cfg.Section("network").Key("ngaPassportUid").String()
	cid := cfg.Section("network").Key("ngaPassportCid").String()
	if !isEmptyOrPlaceholder(uid) && !isEmptyOrPlaceholder(cid) {
		return NetworkConfig{
			BaseURL:      baseURL,
			UserAgent:    ua,
			Cookie:       formatNGACookie(uid, cid),
			CookieSource: "config.ini",
		}, nil
	}

	if cfg.Section("network").Key("cookie_from_chrome").MustBool(true) {
		cookie, source, err := LoadChromeNGACookie(baseURL)
		if err == nil {
			return NetworkConfig{
				BaseURL:      baseURL,
				UserAgent:    ua,
				Cookie:       cookie,
				CookieSource: source,
			}, nil
		}
		return NetworkConfig{}, fmt.Errorf("未配置 NGA Cookie，且从 Chrome 读取失败: %v", err)
	}

	return NetworkConfig{}, fmt.Errorf("配置项配置错误: ngaPassportUid=%s ngaPassportCid=%s", uid, cid)
}

func isEmptyOrPlaceholder(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.Contains(value, "MODIFY_ME")
}

func formatNGACookie(uid, cid string) string {
	return "ngaPassportUid=" + strings.TrimSpace(uid) + ";ngaPassportCid=" + strings.TrimSpace(cid)
}

// LoadChromeNGACookie 从本机 Chrome Cookie 数据库读取 NGA 登录 Cookie。
func LoadChromeNGACookie(baseURL string) (string, string, error) {
	candidates := chromeCookieDBCandidates()
	if len(candidates) == 0 {
		return "", "", errors.New("未找到 Chrome Cookie 数据库")
	}

	var lastErr error
	for _, db := range candidates {
		cookies, err := readChromeCookies(db, baseURL)
		if err != nil {
			lastErr = err
			continue
		}
		uid := cookies["ngaPassportUid"]
		cid := cookies["ngaPassportCid"]
		if uid != "" && cid != "" {
			return formatNGACookie(uid, cid), "Chrome " + db.profile, nil
		}
		lastErr = fmt.Errorf("%s 中未找到完整 NGA Cookie", db.profile)
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", errors.New("未找到完整 NGA Cookie")
}

func readChromeCookies(db chromeCookieDB, baseURL string) (map[string]string, error) {
	tmpDir, err := os.MkdirTemp("", "ngapost2md-chrome-cookies-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	tmpPath := filepath.Join(tmpDir, "Cookies")
	if err := copyFile(db.path, tmpPath); err != nil {
		return nil, err
	}

	query := chromeCookieQuery(baseURL)
	out, err := exec.Command("sqlite3", "-separator", "\t", tmpPath, query).Output()
	if err != nil {
		return nil, fmt.Errorf("sqlite3 读取失败: %v", err)
	}

	result := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			continue
		}
		host, name, plainValue, encryptedHex := fields[0], fields[1], fields[2], fields[3]
		if result[name] != "" {
			continue
		}
		value, err := decryptChromeCookieValue(host, plainValue, encryptedHex, db.keychainService)
		if err == nil && value != "" {
			result[name] = value
		}
	}
	return result, nil
}

func chromeCookieQuery(baseURL string) string {
	host := "bbs.nga.cn"
	if u, err := url.Parse(baseURL); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	domainParts := []string{"%nga.cn", "%nga.178.com", "%ngabbs.com", "%178.com"}
	if host != "" && !strings.Contains(host, "nga") && !strings.Contains(host, "178.com") {
		domainParts = append(domainParts, "%"+escapeSQLLike(host))
	}

	var hostConditions []string
	for _, part := range domainParts {
		hostConditions = append(hostConditions, "host_key LIKE '"+part+"'")
	}

	return "SELECT host_key,name,value,hex(encrypted_value) FROM cookies " +
		"WHERE name IN ('ngaPassportUid','ngaPassportCid') AND (" +
		strings.Join(hostConditions, " OR ") +
		") ORDER BY last_access_utc DESC, creation_utc DESC;"
}

func escapeSQLLike(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "'", "''"), "%", "\\%")
}

func decryptChromeCookieValue(host, plainValue, encryptedHex, keychainService string) (string, error) {
	if plainValue != "" {
		return plainValue, nil
	}
	if encryptedHex == "" {
		return "", errors.New("encrypted_value 为空")
	}
	encryptedValue, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return "", err
	}
	if len(encryptedValue) == 0 {
		return "", errors.New("encrypted_value 为空")
	}

	switch runtime.GOOS {
	case "darwin":
		return decryptMacChromeCookie(host, encryptedValue, keychainService)
	case "linux":
		return decryptLinuxChromeCookie(host, encryptedValue)
	default:
		return "", fmt.Errorf("当前系统 %s 暂不支持解密 Chrome Cookie", runtime.GOOS)
	}
}

func decryptMacChromeCookie(host string, encryptedValue []byte, keychainService string) (string, error) {
	password, err := chromeSafeStoragePassword(keychainService)
	if err != nil {
		return "", err
	}
	key := pbkdf2.Key([]byte(password), []byte("saltysalt"), 1003, 16, sha1.New)
	return decryptChromeAESCBC(host, encryptedValue, key)
}

func decryptLinuxChromeCookie(host string, encryptedValue []byte) (string, error) {
	key := pbkdf2.Key([]byte("peanuts"), []byte("saltysalt"), 1, 16, sha1.New)
	return decryptChromeAESCBC(host, encryptedValue, key)
}

func chromeSafeStoragePassword(service string) (string, error) {
	services := []string{service, "Chrome Safe Storage", "Chromium Safe Storage"}
	for _, s := range services {
		if s == "" {
			continue
		}
		out, err := exec.Command("security", "find-generic-password", "-w", "-s", s).Output()
		if err == nil {
			password := strings.TrimSpace(string(out))
			if password != "" {
				return password, nil
			}
		}
	}
	return "", errors.New("无法从 Keychain 获取 Chrome Safe Storage 密钥")
}

func decryptChromeAESCBC(host string, encryptedValue, key []byte) (string, error) {
	if bytes.HasPrefix(encryptedValue, []byte("v10")) ||
		bytes.HasPrefix(encryptedValue, []byte("v11")) ||
		bytes.HasPrefix(encryptedValue, []byte("v20")) {
		encryptedValue = encryptedValue[3:]
	}
	if len(encryptedValue)%aes.BlockSize != 0 {
		return "", errors.New("Chrome Cookie 密文长度无效")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	decrypted := make([]byte, len(encryptedValue))
	iv := bytes.Repeat([]byte(" "), aes.BlockSize)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(decrypted, encryptedValue)

	decrypted, err = pkcs7Unpad(decrypted)
	if err != nil {
		return "", err
	}

	hostHash := sha256.Sum256([]byte(host))
	if bytes.HasPrefix(decrypted, hostHash[:]) {
		decrypted = decrypted[len(hostHash):]
	}
	return string(decrypted), nil
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("解密结果为空")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(data) {
		return nil, errors.New("Chrome Cookie padding 无效")
	}
	for _, v := range data[len(data)-padding:] {
		if int(v) != padding {
			return nil, errors.New("Chrome Cookie padding 无效")
		}
	}
	return data[:len(data)-padding], nil
}

func chromeCookieDBCandidates() []chromeCookieDB {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var roots []chromeCookieDB
	switch runtime.GOOS {
	case "darwin":
		roots = []chromeCookieDB{
			{
				path:            filepath.Join(home, "Library/Application Support/Google/Chrome"),
				profile:         "Google Chrome",
				keychainService: "Chrome Safe Storage",
			},
			{
				path:            filepath.Join(home, "Library/Application Support/Chromium"),
				profile:         "Chromium",
				keychainService: "Chromium Safe Storage",
			},
		}
	case "linux":
		roots = []chromeCookieDB{
			{path: filepath.Join(home, ".config/google-chrome"), profile: "Google Chrome"},
			{path: filepath.Join(home, ".config/chromium"), profile: "Chromium"},
		}
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			roots = []chromeCookieDB{
				{path: filepath.Join(localAppData, "Google/Chrome/User Data"), profile: "Google Chrome"},
			}
		}
	}

	var candidates []chromeCookieDB
	for _, root := range roots {
		candidates = append(candidates, profileCookieDBs(root)...)
	}
	return candidates
}

func profileCookieDBs(root chromeCookieDB) []chromeCookieDB {
	entries, err := os.ReadDir(root.path)
	if err != nil {
		return nil
	}

	var profiles []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "Default" || strings.HasPrefix(name, "Profile ") {
			profiles = append(profiles, name)
		}
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i] == "Default" {
			return true
		}
		if profiles[j] == "Default" {
			return false
		}
		return profiles[i] < profiles[j]
	})

	var candidates []chromeCookieDB
	for _, profile := range profiles {
		for _, cookiePath := range []string{
			filepath.Join(root.path, profile, "Network", "Cookies"),
			filepath.Join(root.path, profile, "Cookies"),
		} {
			if info, err := os.Stat(cookiePath); err == nil && !info.IsDir() {
				candidates = append(candidates, chromeCookieDB{
					path:            cookiePath,
					profile:         root.profile + "/" + profile,
					keychainService: root.keychainService,
				})
				break
			}
		}
	}
	return candidates
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
