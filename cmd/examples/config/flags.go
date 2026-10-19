package config

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

var DataDir string
var OpenRouterKey = ""

func init() {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("failed to resolve config path")
	}

	currentDir := filepath.Dir(currentFile)

	baseDir := filepath.Join(currentDir, "..")

	DataDir = filepath.Join(baseDir, "data")

	fmt.Printf("[CONFIG] Resolved DataDir: %s\n", DataDir)
}

func InitFlags() {
	flag.StringVar(&DataDir, "data-dir", DataDir, "thư mục chứa dữ liệu đầu vào")
}

func ReadDataFromFile(fileName string) (string, error) {
	log.Printf("ReadDataFromFile: %s calltime %s", fileName, time.DateTime)
	absPath := filepath.Join(DataDir, fileName)

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file [%s]: %w", absPath, err)
	}

	return string(data), nil
}

func GetListFileInStore() ([]string, error) {
	log.Printf("GetListFileInStore calltime %s", time.DateTime)
	var listFile = make([]string, 0)

	files, err := os.ReadDir(DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory [%s]: %w", DataDir, err)
	}
	for _, file := range files {
		listFile = append(listFile, filepath.Join(DataDir, file.Name()))
	}
	log.Printf("GetListFileInStore found %d items", len(listFile))

	return listFile, nil
}

func WriteDataToFile(nameFile string, content string) error {
	log.Printf("WriteDataToFile calltime %s", time.Now().Format(time.DateTime))

	absPath := filepath.Join(DataDir, nameFile)

	// Tạo thư mục cha nếu chưa tồn tại
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory [%s]: %w", dir, err)
	}

	err := os.WriteFile(absPath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("failed to write file [%s]: %w", absPath, err)
	}

	log.Printf("WriteDataToFile successfully wrote to [%s]", absPath)
	return nil
}
