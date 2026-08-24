package nya

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// GenerateRecoveryVolumes 生成恢复卷
// archivePath: .nya归档路径
// count: 恢复卷数量
func GenerateRecoveryVolumes(archivePath string, count int) error {
	data, err := os.ReadFile(archivePath)
	if err != nil { return err }

	// 每个恢复卷 = 对归档数据做XOR分组
	// 简单方案: 每个恢复卷是不同seed的XOR校验
	volSize := len(data)
	
	base := strings.TrimSuffix(archivePath, filepath.Ext(archivePath))
	
	for i := 0; i < count; i++ {
		vol := make([]byte, volSize)
		
		// 用不同seed生成不同的校验数据
		seed := sha256.Sum256([]byte(fmt.Sprintf("nyarc-recovery-%d", i)))
		
		for j := 0; j < len(data); j++ {
			vol[j] = data[j] ^ seed[j%32]
		}
		
		// 写恢复卷
		volPath := fmt.Sprintf("%s.r%02d", base, i+1)
		
		// 恢复卷头: [magic 4B][volIndex 4B][totalVols 4B][dataSize 8B][dataSHA256 32B]
		header := make([]byte, 52)
		copy(header[:4], "NYAR") // NYA Recovery
		binary.LittleEndian.PutUint32(header[4:8], uint32(i+1))
		binary.LittleEndian.PutUint32(header[8:12], uint32(count))
		binary.LittleEndian.PutUint64(header[12:20], uint64(len(data)))
		hash := sha256.Sum256(data)
		copy(header[20:52], hash[:])
		
		f, err := os.Create(volPath)
		if err != nil { return err }
		f.Write(header)
		f.Write(vol)
		f.Close()
		
		fmt.Printf("  ✅ %s (%s)\n", volPath, HumanSize(len(vol)+52))
	}
	
	return nil
}

// RecoverFromVolumes 从恢复卷恢复损坏的归档
func RecoverFromVolumes(archivePath string) error {
	data, err := os.ReadFile(archivePath)
	if err != nil { return fmt.Errorf("cannot read archive: %v", err) }
	
	// 查找恢复卷
	base := strings.TrimSuffix(archivePath, filepath.Ext(archivePath))
	
	for i := 1; i <= 99; i++ {
		volPath := fmt.Sprintf("%s.r%02d", base, i)
		volData, err := os.ReadFile(volPath)
		if err != nil { break }
		
		if len(volData) < 52 { continue }
		if string(volData[:4]) != "NYAR" { continue }
		
		// 读header
		volIdx := binary.LittleEndian.Uint32(volData[4:8])
		dataSize := binary.LittleEndian.Uint64(volData[12:20])
		var expectedHash [32]byte
		copy(expectedHash[:], volData[20:52])
		
		volPayload := volData[52:]
		_ = volIdx
		_ = dataSize
		
		// 验证当前归档是否损坏
		actualHash := sha256.Sum256(data)
		if actualHash == expectedHash {
			fmt.Println("✅ Archive OK, no recovery needed")
			return nil
		}
		
		// 用恢复卷XOR恢复
		seed := sha256.Sum256([]byte(fmt.Sprintf("nyarc-recovery-%d", i-1)))
		recovered := make([]byte, len(data))
		for j := 0; j < len(data) && j < len(volPayload); j++ {
			recovered[j] = volPayload[j] ^ seed[j%32]
		}
		
		// 验证恢复结果
		recoveredHash := sha256.Sum256(recovered)
		if recoveredHash == expectedHash {
			os.WriteFile(archivePath, recovered, 0644)
			fmt.Printf("✅ Archive recovered from %s\n", volPath)
			return nil
		}
	}
	
	return fmt.Errorf("no valid recovery volumes found")
}

// ListRecoveryVolumes 列出恢复卷
func ListRecoveryVolumes(archivePath string) {
	base := strings.TrimSuffix(archivePath, filepath.Ext(archivePath))
	found := 0
	for i := 1; i <= 99; i++ {
		volPath := fmt.Sprintf("%s.r%02d", base, i)
		info, err := os.Stat(volPath)
		if err != nil { break }
		fmt.Printf("  Recovery %02d: %s (%s)\n", i, volPath, HumanSize(int(info.Size())))
		found++
	}
	if found == 0 {
		fmt.Println("  No recovery volumes found")
	} else {
		fmt.Printf("  Total: %d recovery volumes\n", found)
	}
}

func init() { _ = io.ReadAll }
