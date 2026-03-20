package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ethan03805/axiom/internal/broker"
	"github.com/ethan03805/axiom/internal/engine"
	"github.com/spf13/cobra"
)

// --- axiom bitnet ---

var bitnetCmd = &cobra.Command{
	Use:   "bitnet",
	Short: "Manage the local BitNet inference server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// newBitNetServerFromConfig loads config and creates a BitNetServer instance.
func newBitNetServerFromConfig() (*broker.BitNetServer, *engine.Config, error) {
	cfg, err := engine.LoadConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	srv := broker.NewBitNetServer(broker.BitNetServerConfig{
		Enabled:               cfg.BitNet.Enabled,
		Host:                  cfg.BitNet.Host,
		Port:                  cfg.BitNet.Port,
		MaxConcurrentRequests: cfg.BitNet.MaxConcurrentRequests,
		CPUThreads:            cfg.BitNet.CPUThreads,
		BinaryPath:            cfg.BitNet.BinaryPath,
		ModelsDir:             cfg.BitNet.ModelsDir,
	})
	return srv, cfg, nil
}

var bitnetStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the local inference server",
	Long: `Starts the BitNet inference server with Falcon3 1-bit weights.
On first run, downloads model weights if not present (with user confirmation).
The server exposes an OpenAI-compatible API at the configured port.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		srv, cfg, err := newBitNetServerFromConfig()
		if err != nil {
			return err
		}

		// Check if weights are present.
		hasWeights, err := srv.EnsureWeights()
		if err != nil {
			return fmt.Errorf("check model weights: %w", err)
		}

		if !hasWeights {
			fmt.Println("Falcon3 1.58-bit model weights not found.")
			fmt.Println("This will download and convert the model using the vendored BitNet framework.")
			fmt.Println("Approximate download size: ~1.3 GB")
			fmt.Print("Proceed? (y/n) ")
			reader := bufio.NewReader(os.Stdin)
			answer, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("read input: %w", err)
			}
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				fmt.Println("Download cancelled. Cannot start without model weights.")
				return nil
			}

			if err := downloadFalcon3Model(cfg); err != nil {
				return fmt.Errorf("download model: %w", err)
			}
		}

		fmt.Println("Starting BitNet inference server...")
		if err := srv.Start(); err != nil {
			if broker.NeedsFirstRun(err) {
				fmt.Println(err.Error())
				return nil
			}
			return fmt.Errorf("start server: %w", err)
		}

		status := srv.Status()
		fmt.Println("BitNet server started successfully.")
		fmt.Printf("  Host:       %s\n", status.Host)
		fmt.Printf("  Port:       %d\n", status.Port)
		fmt.Printf("  Threads:    %d\n", status.CPUThreads)
		fmt.Printf("  Running:    %v\n", status.Running)
		return nil
	},
}

var bitnetStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the local inference server",
	RunE: func(cmd *cobra.Command, args []string) error {
		srv, _, err := newBitNetServerFromConfig()
		if err != nil {
			return err
		}

		if err := srv.Stop(); err != nil {
			return fmt.Errorf("stop server: %w", err)
		}

		fmt.Println("BitNet server stopped.")
		return nil
	},
}

var bitnetStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show server status, resource usage, active requests",
	RunE: func(cmd *cobra.Command, args []string) error {
		srv, _, err := newBitNetServerFromConfig()
		if err != nil {
			return err
		}

		status := srv.Status()
		usage := srv.GetResourceUsage()

		fmt.Println("BitNet Server Status")
		fmt.Println("--------------------")
		if status.Running {
			fmt.Println("Status:          running")
			fmt.Printf("Uptime:          %s\n", status.Uptime.Truncate(time.Second))
		} else {
			fmt.Println("Status:          stopped")
		}
		fmt.Printf("Enabled:         %v\n", status.Enabled)
		fmt.Printf("Host:            %s\n", status.Host)
		fmt.Printf("Port:            %d\n", status.Port)
		fmt.Printf("CPU Threads:     %d / %d\n", status.CPUThreads, usage.TotalCPUs)
		fmt.Printf("CPU Usage:       %.1f%%\n", usage.CPUPercent)
		fmt.Printf("Active Requests: %d\n", status.ActiveRequests)

		hasWeights, _ := srv.EnsureWeights()
		if hasWeights {
			fmt.Println("Model Weights:   present")
		} else {
			fmt.Println("Model Weights:   not found")
		}
		return nil
	},
}

var bitnetModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List available local models",
	RunE: func(cmd *cobra.Command, args []string) error {
		srv, _, err := newBitNetServerFromConfig()
		if err != nil {
			return err
		}

		models, err := srv.ListModels()
		if err != nil {
			return fmt.Errorf("list models: %w", err)
		}

		fmt.Println("Local Models")
		fmt.Println("------------")
		if len(models) == 0 {
			fmt.Printf("No models found in %s\n", srv.GetModelsDir())
			fmt.Println("Run 'axiom bitnet start' to download Falcon3 1-bit weights.")
			return nil
		}
		fmt.Printf("Models directory: %s\n", srv.GetModelsDir())
		fmt.Println()
		for _, m := range models {
			fmt.Printf("  %s\n", m)
		}
		return nil
	},
}

// downloadFalcon3Model uses the vendored BitNet setup_env.py to download
// and convert the Falcon3 1.58-bit model to GGUF format.
//
// After the initial build from setup_env.py, this function performs a follow-up
// cmake rebuild with the correct ARM flags on Apple Silicon. The upstream
// setup_env.py builds with BITNET_ARM_TL1=OFF, which omits the optimized ARM
// TL1 kernels and can cause SIGSEGV during inference (BUG-042).
func downloadFalcon3Model(cfg *engine.Config) error {
	repo := cfg.BitNet.ModelRepo
	if repo == "" {
		repo = "tiiuae/Falcon3-1B-Instruct-1.58bit"
	}

	// Find the vendored BitNet directory
	bitnetDir := resolveBitNetDir()
	if bitnetDir == "" {
		return fmt.Errorf("vendored BitNet directory not found; ensure third_party/BitNet exists relative to the axiom binary or working directory")
	}

	setupScript := filepath.Join(bitnetDir, "setup_env.py")
	if _, err := os.Stat(setupScript); err != nil {
		return fmt.Errorf("BitNet setup_env.py not found at %s: %w", setupScript, err)
	}

	// Check for Python venv; if not present, create one and install deps
	venvPython := filepath.Join(bitnetDir, ".venv", "bin", "python3")
	if _, err := os.Stat(venvPython); err != nil {
		fmt.Println("Setting up Python environment for BitNet...")
		venvCmd := exec.Command("python3", "-m", "venv", filepath.Join(bitnetDir, ".venv"))
		venvCmd.Stdout = os.Stdout
		venvCmd.Stderr = os.Stderr
		if err := venvCmd.Run(); err != nil {
			return fmt.Errorf("create Python venv: %w", err)
		}
		pipCmd := exec.Command(filepath.Join(bitnetDir, ".venv", "bin", "pip"), "install", "-r", filepath.Join(bitnetDir, "requirements.txt"))
		pipCmd.Dir = bitnetDir
		pipCmd.Stdout = os.Stdout
		pipCmd.Stderr = os.Stderr
		if err := pipCmd.Run(); err != nil {
			return fmt.Errorf("install BitNet requirements: %w", err)
		}
	}

	fmt.Printf("Downloading and converting %s...\n", repo)
	fmt.Println("This may take several minutes.")

	downloadCmd := exec.Command(venvPython, "setup_env.py", "--hf-repo", repo, "-q", "i2_s")
	downloadCmd.Dir = bitnetDir
	downloadCmd.Stdout = os.Stdout
	downloadCmd.Stderr = os.Stderr
	if err := downloadCmd.Run(); err != nil {
		return fmt.Errorf("BitNet setup_env.py failed: %w", err)
	}

	// BUG-042 fix: The upstream setup_env.py builds with BITNET_ARM_TL1=OFF on
	// ARM64, which omits the optimized 1-bit TL1 kernels. Without these, the
	// i2_s quantized model crashes with SIGSEGV during token generation on Apple
	// Silicon. Perform a follow-up cmake rebuild with the correct ARM flags.
	if runtime.GOARCH == "arm64" {
		fmt.Println("Rebuilding BitNet binary with ARM TL1 kernel support...")
		if err := rebuildBitNetARM(bitnetDir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: ARM TL1 rebuild failed: %v (inference may crash)\n", err)
			// Don't fail the entire download -- the model is usable with cloud fallback.
		} else {
			fmt.Println("BitNet binary rebuilt with ARM TL1 support.")
		}
	}

	fmt.Println("Model downloaded and converted successfully.")
	return nil
}

// rebuildBitNetARM performs a cmake rebuild of the BitNet binary with
// BITNET_ARM_TL1=ON for Apple Silicon / ARM64 platforms. This enables the
// optimized 1-bit TL1 compute kernels that the upstream build omits.
//
// The kernel header (include/bitnet-lut-kernels.h) must already exist from
// the prior setup_env.py run which calls codegen_tl1.py.
func rebuildBitNetARM(bitnetDir string) error {
	// Verify the kernel header exists (generated by setup_env.py's gen_code step).
	kernelHeader := filepath.Join(bitnetDir, "include", "bitnet-lut-kernels.h")
	if _, err := os.Stat(kernelHeader); err != nil {
		return fmt.Errorf("TL1 kernel header not found at %s (was setup_env.py's codegen step skipped?): %w", kernelHeader, err)
	}

	// Reconfigure cmake with ARM TL1 enabled.
	cmakeConfigure := exec.Command("cmake",
		"-B", "build",
		"-DBITNET_ARM_TL1=ON",
		"-DGGML_METAL=ON",
		"-DCMAKE_BUILD_TYPE=Release",
		"-DCMAKE_C_COMPILER=clang",
		"-DCMAKE_CXX_COMPILER=clang++",
	)
	cmakeConfigure.Dir = bitnetDir
	cmakeConfigure.Stdout = os.Stdout
	cmakeConfigure.Stderr = os.Stderr
	if err := cmakeConfigure.Run(); err != nil {
		return fmt.Errorf("cmake configure: %w", err)
	}

	// Build.
	cmakeBuild := exec.Command("cmake", "--build", "build", "--config", "Release")
	cmakeBuild.Dir = bitnetDir
	cmakeBuild.Stdout = os.Stdout
	cmakeBuild.Stderr = os.Stderr
	if err := cmakeBuild.Run(); err != nil {
		return fmt.Errorf("cmake build: %w", err)
	}

	return nil
}

// resolveBitNetDir finds the vendored BitNet directory under third_party/.
func resolveBitNetDir() string {
	// Try relative to the axiom binary
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates := []string{
			filepath.Join(exeDir, "..", "third_party", "BitNet"),
			filepath.Join(exeDir, "third_party", "BitNet"),
		}
		for _, c := range candidates {
			if abs, err := filepath.Abs(c); err == nil {
				if info, err := os.Stat(abs); err == nil && info.IsDir() {
					return abs
				}
			}
		}
	}
	// Try relative to working directory
	if abs, err := filepath.Abs(filepath.Join("third_party", "BitNet")); err == nil {
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}
	return ""
}

func init() {
	bitnetCmd.AddCommand(bitnetStartCmd)
	bitnetCmd.AddCommand(bitnetStopCmd)
	bitnetCmd.AddCommand(bitnetStatusCmd)
	bitnetCmd.AddCommand(bitnetModelsCmd)
}
