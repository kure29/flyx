package kernel

import (
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// elfMachines maps GOARCH to the ELF machine a binary must be built for.
var elfMachines = map[string]elf.Machine{
	"amd64":   elf.EM_X86_64,
	"arm64":   elf.EM_AARCH64,
	"arm":     elf.EM_ARM,
	"386":     elf.EM_386,
	"riscv64": elf.EM_RISCV,
	"loong64": elf.EM_LOONGARCH,
}

// checkRunnable reports why a Linux binary cannot run on this machine,
// without running it: a binary built for another CPU, or one that needs a
// dynamic loader the system lacks (a glibc build on musl systems such as
// Alpine), fails to exec with "exec format error" or "no such file or
// directory". Anything that is not ELF is left for exec to judge.
func checkRunnable(path string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	return checkELF(path, runtime.GOARCH, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	})
}

func checkELF(path, goarch string, exists func(string) bool) error {
	f, err := elf.Open(path)
	if err != nil {
		var fe *elf.FormatError
		if errors.As(err, &fe) {
			return nil // a script or other non-ELF file
		}
		return err
	}
	defer f.Close()
	if want, ok := elfMachines[goarch]; ok && f.Machine != want {
		return fmt.Errorf("文件是 %s 架构的，本机是 %s", strings.TrimPrefix(f.Machine.String(), "EM_"), goarch)
	}
	for _, p := range f.Progs {
		if p.Type != elf.PT_INTERP {
			continue
		}
		buf := make([]byte, p.Filesz)
		if _, err := p.ReadAt(buf, 0); err != nil {
			return fmt.Errorf("读取动态加载器失败: %v", err)
		}
		interp := strings.TrimRight(string(buf), "\x00")
		if interp != "" && !exists(interp) {
			hint := ""
			if strings.Contains(interp, "ld-linux") {
				hint = "（这是 glibc 版本，本机可能是 Alpine 等 musl 系统）"
			}
			return fmt.Errorf("本机缺少它需要的动态加载器 %s%s", interp, hint)
		}
	}
	return nil
}
