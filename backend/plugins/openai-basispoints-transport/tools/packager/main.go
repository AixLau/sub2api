package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type packageOptions struct {
	pluginDir  string
	output     string
	targets    string
	signingKey string
	keyID      string
}

func main() {
	var options packageOptions
	flag.StringVar(&options.pluginDir, "plugin-dir", ".", "插件工程目录")
	flag.StringVar(&options.output, "output", "plugin.s2plugin", "输出包路径")
	flag.StringVar(&options.targets, "targets", "linux-amd64", "逗号分隔的目标平台")
	flag.StringVar(&options.signingKey, "signing-key", "", "Ed25519 私钥路径（可选）")
	flag.StringVar(&options.keyID, "key-id", "", "签名密钥 ID（可选）")
	flag.Parse()

	if err := packagePlugin(options); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func packagePlugin(options packageOptions) error {
	pluginDir, err := filepath.Abs(options.pluginDir)
	if err != nil {
		return fmt.Errorf("解析插件目录: %w", err)
	}
	if (options.signingKey == "") != (options.keyID == "") {
		return errors.New("signing-key 和 key-id 必须同时提供")
	}
	targets := splitTargets(options.targets)
	if len(targets) == 0 {
		return errors.New("至少需要一个目标平台")
	}
	manifestBytes, files, err := buildManifest(pluginDir, targets)
	if err != nil {
		return err
	}
	entries := map[string][]byte{"manifest.json": manifestBytes}
	for path, data := range files {
		entries[path] = data
	}
	if options.signingKey != "" {
		privateKey, err := readPrivateKey(options.signingKey)
		if err != nil {
			return err
		}
		signature := map[string]string{
			"algorithm": "ed25519",
			"key_id":    options.keyID,
			"signature": base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, manifestBytes)),
		}
		signatureBytes, err := json.Marshal(signature)
		if err != nil {
			return err
		}
		entries["signature.json"] = signatureBytes
	}
	if err := os.MkdirAll(filepath.Dir(options.output), 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("创建输出目录: %w", err)
	}
	output, err := os.Create(options.output)
	if err != nil {
		return fmt.Errorf("创建插件包: %w", err)
	}
	defer func() { _ = output.Close() }()
	archive := zip.NewWriter(output)
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := writeZipEntry(archive, path, entries[path]); err != nil {
			_ = archive.Close()
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("关闭插件包: %w", err)
	}
	return nil
}

func buildManifest(pluginDir string, targets []string) ([]byte, map[string][]byte, error) {
	data, err := os.ReadFile(filepath.Join(pluginDir, "manifest.source.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("读取 manifest.source.json: %w", err)
	}
	var manifest map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, nil, fmt.Errorf("解析 manifest.source.json: %w", err)
	}
	runtimes := map[string]any{}
	files := map[string]string{}
	artifacts := map[string][]byte{}
	for _, target := range targets {
		binaryName := "openai-basispoints-transport"
		if strings.HasPrefix(target, "windows-") {
			binaryName += ".exe"
		}
		relativeBinary := filepath.ToSlash(filepath.Join(".build", target, binaryName))
		binary, err := os.ReadFile(filepath.Join(pluginDir, filepath.FromSlash(relativeBinary)))
		if err != nil {
			return nil, nil, fmt.Errorf("读取 %s: %w", relativeBinary, err)
		}
		archivePath := filepath.ToSlash(filepath.Join("runtimes", target, "openai-basispoints-transport"))
		if strings.HasSuffix(binaryName, ".exe") {
			archivePath += ".exe"
		}
		runtimes[target] = map[string]string{"path": archivePath}
		files[archivePath] = hashBytes(binary)
		artifacts[archivePath] = binary
	}
	uiRoot := filepath.Join(pluginDir, "ui")
	if err := filepath.WalkDir(uiRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(pluginDir, path)
		if err != nil {
			return err
		}
		archivePath := filepath.ToSlash(relative)
		file, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[archivePath] = hashBytes(file)
		artifacts[archivePath] = file
		return nil
	}); err != nil {
		return nil, nil, fmt.Errorf("读取插件 UI: %w", err)
	}
	manifest["runtimes"] = runtimes
	manifest["files"] = files
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("序列化 manifest.json: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	return manifestBytes, artifacts, nil
}

func writeZipEntry(archive *zip.Writer, path string, data []byte) error {
	header := &zip.FileHeader{Name: path, Method: zip.Deflate}
	header.SetModTime(time.Unix(0, 0).UTC())
	if strings.HasPrefix(path, "runtimes/") {
		header.SetMode(0o755)
	} else {
		header.SetMode(0o644)
	}
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("创建插件文件 %s: %w", path, err)
	}
	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("写入插件文件 %s: %w", path, err)
	}
	return nil
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取签名私钥: %w", err)
	}
	if block, _ := pem.Decode(raw); block != nil {
		key, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, fmt.Errorf("解析 PKCS#8 私钥: %w", parseErr)
		}
		privateKey, ok := key.(ed25519.PrivateKey)
		if !ok {
			return nil, errors.New("签名私钥不是 Ed25519")
		}
		return privateKey, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(trimmed), nil
	}
	for _, decoder := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if decoded, decodeErr := decoder.DecodeString(string(trimmed)); decodeErr == nil && len(decoded) == ed25519.PrivateKeySize {
			return ed25519.PrivateKey(decoded), nil
		}
	}
	if decoded, decodeErr := hex.DecodeString(string(trimmed)); decodeErr == nil && len(decoded) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(decoded), nil
	}
	return nil, errors.New("签名私钥必须是 Ed25519 原始、Base64、十六进制或 PKCS#8 格式")
}

func splitTargets(raw string) []string {
	seen := map[string]struct{}{}
	var targets []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		targets = append(targets, item)
	}
	sort.Strings(targets)
	return targets
}

func hashBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return fmt.Sprintf("%x", digest[:])
}
