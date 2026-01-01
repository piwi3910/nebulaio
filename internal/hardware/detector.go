// Package hardware provides real hardware detection for GPU, DPU, and RDMA devices.
// This enables NebulaIO to automatically detect and configure acceleration hardware.
package hardware

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// DeviceType represents the type of hardware device.
type DeviceType string

const (
	DeviceTypeGPU  DeviceType = "gpu"
	DeviceTypeDPU  DeviceType = "dpu"
	DeviceTypeRDMA DeviceType = "rdma"
)

const (
	unknownValue   = "unknown"
	commandTimeout = 10 * time.Second
)

// GPUInfo contains information about a detected GPU.
type GPUInfo struct {
	Name         string `json:"name"`
	UUID         string `json:"uuid"`
	Driver       string `json:"driver_version"`
	CUDAVersion  string `json:"cuda_version"`
	ComputeCap   string `json:"compute_capability"`
	Index        int    `json:"index"`
	MemoryTotal  uint64 `json:"memory_total"`
	MemoryFree   uint64 `json:"memory_free"`
	GDSSupported bool   `json:"gds_supported"`
	P2PSupported bool   `json:"p2p_supported"`
}

// DPUInfo contains information about a detected DPU/SmartNIC.
type DPUInfo struct {
	Name            string   `json:"name"`
	Vendor          string   `json:"vendor"`
	DevicePath      string   `json:"device_path"`
	FirmwareVer     string   `json:"firmware_version"`
	Capabilities    []string `json:"capabilities"`
	Index           int      `json:"index"`
	CryptoSupport   bool     `json:"crypto_support"`
	CompressSupport bool     `json:"compress_support"`
	RDMASupport     bool     `json:"rdma_support"`
}

// RDMAInfo contains information about a detected RDMA device.
type RDMAInfo struct {
	Name          string `json:"name"`
	DevicePath    string `json:"device_path"`
	NodeGUID      string `json:"node_guid"`
	SysImageGUID  string `json:"sys_image_guid"`
	BoardID       string `json:"board_id"`
	FirmwareVer   string `json:"firmware_version"`
	NodeType      string `json:"node_type"`
	LinkLayer     string `json:"link_layer"`
	State         string `json:"state"`
	PhysPortCount int    `json:"phys_port_count"`
	Speed         uint64 `json:"speed"`
}

// HardwareCapabilities represents detected hardware capabilities.
type HardwareCapabilities struct {
	LastUpdated   time.Time  `json:"last_updated"`
	GPUs          []GPUInfo  `json:"gpus"`
	DPUs          []DPUInfo  `json:"dpus"`
	RDMADevices   []RDMAInfo `json:"rdma_devices"`
	GPUAvailable  bool       `json:"gpu_available"`
	DPUAvailable  bool       `json:"dpu_available"`
	RDMAAvailable bool       `json:"rdma_available"`
}

// Detector handles hardware detection operations.
type Detector struct {
	capabilities *HardwareCapabilities
	stopCh       chan struct{}
	refreshRate  time.Duration
	mu           sync.RWMutex
}

// NewDetector creates a new hardware detector.
func NewDetector() *Detector {
	return &Detector{
		capabilities: &HardwareCapabilities{},
		refreshRate:  30 * time.Second,
		stopCh:       make(chan struct{}),
	}
}

// Start begins periodic hardware detection.
func (d *Detector) Start() {
	// Initial detection
	d.Refresh()

	// Periodic refresh
	go func() {
		ticker := time.NewTicker(d.refreshRate)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				d.Refresh()
			case <-d.stopCh:
				return
			}
		}
	}()
}

// Stop stops periodic hardware detection.
func (d *Detector) Stop() {
	close(d.stopCh)
}

// Refresh updates hardware detection results.
func (d *Detector) Refresh() {
	d.mu.Lock()
	defer d.mu.Unlock()

	log.Debug().Msg("Refreshing hardware detection")

	d.capabilities.GPUs = d.detectGPUs()
	d.capabilities.DPUs = d.detectDPUs()
	d.capabilities.RDMADevices = d.detectRDMADevices()

	d.capabilities.GPUAvailable = len(d.capabilities.GPUs) > 0
	d.capabilities.DPUAvailable = len(d.capabilities.DPUs) > 0
	d.capabilities.RDMAAvailable = len(d.capabilities.RDMADevices) > 0

	d.capabilities.LastUpdated = time.Now()

	log.Info().
		Int("gpus", len(d.capabilities.GPUs)).
		Int("dpus", len(d.capabilities.DPUs)).
		Int("rdma_devices", len(d.capabilities.RDMADevices)).
		Msg("Hardware detection completed")
}

// GetCapabilities returns current hardware capabilities.
func (d *Detector) GetCapabilities() HardwareCapabilities {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return *d.capabilities
}

// HasGPU returns true if GPU is available.
func (d *Detector) HasGPU() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.capabilities.GPUAvailable
}

// HasDPU returns true if DPU is available.
func (d *Detector) HasDPU() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.capabilities.DPUAvailable
}

// HasRDMA returns true if RDMA is available.
func (d *Detector) HasRDMA() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.capabilities.RDMAAvailable
}

// detectGPUs detects NVIDIA GPUs using nvidia-smi.
func (d *Detector) detectGPUs() []GPUInfo {
	var gpus []GPUInfo

	// Check if nvidia-smi is available
	_, err := exec.LookPath("nvidia-smi")
	if err != nil {
		log.Debug().Msg("nvidia-smi not found, GPU detection disabled")
		return gpus
	}

	// Query GPU information
	// All arguments are hardcoded to prevent command injection
	// INTENTIONAL: Using context.Background() for hardware detection.
	// This is called during system initialization, not tied to any request.
	// The timeout provides the cancellation mechanism for the shell command.
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=index,name,uuid,memory.total,memory.free,driver_version,cuda_version,compute_cap",
		"--format=csv,noheader,nounits")

	output, err := cmd.Output()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to query GPU information")
		return gpus
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()

		fields := strings.Split(line, ", ")
		if len(fields) < 8 {
			continue
		}

		index, _ := strconv.Atoi(strings.TrimSpace(fields[0]))
		memTotal, _ := strconv.ParseUint(strings.TrimSpace(fields[3]), 10, 64)
		memFree, _ := strconv.ParseUint(strings.TrimSpace(fields[4]), 10, 64)

		gpu := GPUInfo{
			Index:       index,
			Name:        strings.TrimSpace(fields[1]),
			UUID:        strings.TrimSpace(fields[2]),
			MemoryTotal: memTotal * 1024 * 1024, // Convert MiB to bytes
			MemoryFree:  memFree * 1024 * 1024,
			Driver:      strings.TrimSpace(fields[5]),
			CUDAVersion: strings.TrimSpace(fields[6]),
			ComputeCap:  strings.TrimSpace(fields[7]),
		}

		// Check for GDS support (requires CUDA 11.4+ and supported GPU)
		gpu.GDSSupported = d.checkGDSSupport(gpu)

		// Check for P2P support
		gpu.P2PSupported = d.checkP2PSupport(index)

		gpus = append(gpus, gpu)
	}

	return gpus
}

// checkGDSSupport checks if GPU Direct Storage is supported.
func (d *Detector) checkGDSSupport(gpu GPUInfo) bool {
	// GDS requires:
	// 1. CUDA 11.4 or later
	// 2. Compute capability 7.0 or higher (Volta, Turing, Ampere, Hopper)
	// 3. GDS libraries installed

	// Check CUDA version
	cudaVer := strings.Split(gpu.CUDAVersion, ".")
	if len(cudaVer) >= 2 {
		major, _ := strconv.Atoi(cudaVer[0])

		minor, _ := strconv.Atoi(cudaVer[1])
		if major < 11 || (major == 11 && minor < 4) {
			return false
		}
	}

	// Check compute capability
	ccParts := strings.Split(gpu.ComputeCap, ".")
	if len(ccParts) >= 1 {
		major, _ := strconv.Atoi(ccParts[0])
		if major < 7 {
			return false
		}
	}

	// Check for GDS library
	gdsLibPaths := []string{
		"/usr/local/cuda/lib64/libcufile.so",
		"/usr/lib/x86_64-linux-gnu/libcufile.so",
	}

	for _, path := range gdsLibPaths {
		_, err := os.Stat(path)
		if err == nil {
			return true
		}
	}

	return false
}

// checkP2PSupport checks if P2P transfers are supported between GPUs.
func (d *Detector) checkP2PSupport(gpuIndex int) bool {
	// Verify nvidia-smi is available before executing
	_, err := exec.LookPath("nvidia-smi")
	if err != nil {
		log.Debug().Msg("nvidia-smi not found, cannot check P2P support")
		return false
	}

	// Use nvidia-smi to check P2P support
	// All arguments are hardcoded to prevent command injection
	// INTENTIONAL: Using context.Background() for hardware capability check.
	// This is called during system initialization, not tied to any request.
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nvidia-smi", "topo", "-p2p", "r")

	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// Parse output to check if this GPU supports P2P
	return strings.Contains(string(output), "OK")
}

// detectDPUs detects BlueField and other DPUs.
func (d *Detector) detectDPUs() []DPUInfo {
	var dpus []DPUInfo

	// Check for BlueField DPUs via mlxconfig or devinfo
	dpus = append(dpus, d.detectBlueFieldDPUs()...)

	// Check for other DPUs via sysfs
	dpus = append(dpus, d.detectSysfsDPUs()...)

	return dpus
}

// detectBlueFieldDPUs detects NVIDIA BlueField DPUs.
func (d *Detector) detectBlueFieldDPUs() []DPUInfo {
	var dpus []DPUInfo

	// Check if mst is available (Mellanox Software Tools)
	_, err := exec.LookPath("mst")
	if err != nil {
		return dpus
	}

	// Start MST
	// All arguments are hardcoded to prevent command injection
	// INTENTIONAL: Using context.Background() for DPU detection during initialization.
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	_ = exec.CommandContext(ctx, "mst", "start").Run()

	// Query devices
	// All arguments are hardcoded to prevent command injection
	cmd := exec.CommandContext(ctx, "mst", "status", "-v")

	output, err := cmd.Output()
	if err != nil {
		log.Debug().Err(err).Msg("MST status query failed")
		return dpus
	}

	// Parse MST output for BlueField devices
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	bfPattern := regexp.MustCompile(`BlueField[-\s](\d+)`)
	index := 0

	for scanner.Scan() {
		line := scanner.Text()
		if match := bfPattern.FindStringSubmatch(line); match != nil {
			dpu := DPUInfo{
				Index:           index,
				Name:            "BlueField-" + match[1],
				Vendor:          "NVIDIA",
				CryptoSupport:   true, // BlueField has crypto offload
				CompressSupport: true, // BlueField has compression offload
				RDMASupport:     true, // BlueField has native RDMA
				Capabilities:    []string{"crypto", "compression", "rdma", "nvme"},
			}

			// Try to get firmware version
			dpu.FirmwareVer = d.getBlueFieldFirmware(index)

			dpus = append(dpus, dpu)
			index++
		}
	}

	return dpus
}

// getBlueFieldFirmware gets BlueField firmware version.
func (d *Detector) getBlueFieldFirmware(index int) string {
	// Verify mlxfwmanager is available before executing
	_, err := exec.LookPath("mlxfwmanager")
	if err != nil {
		log.Debug().Msg("mlxfwmanager not found, cannot query firmware version")
		return unknownValue
	}

	// Query firmware version
	// All arguments are hardcoded to prevent command injection
	// INTENTIONAL: Using context.Background() for firmware version query.
	// This is called during system initialization or on-demand detection.
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "mlxfwmanager", "--query")

	output, err := cmd.Output()
	if err != nil {
		return unknownValue
	}

	// Parse firmware version from output
	fwPattern := regexp.MustCompile(`FW Version:\s+(\S+)`)
	if match := fwPattern.FindStringSubmatch(string(output)); match != nil {
		return match[1]
	}

	return unknownValue
}

// detectSysfsDPUs detects DPUs via sysfs.
func (d *Detector) detectSysfsDPUs() []DPUInfo {
	var dpus []DPUInfo

	// Check for DPU devices in sysfs
	dpuPath := "/sys/class/infiniband"

	entries, err := os.ReadDir(dpuPath)
	if err != nil {
		return dpus
	}

	for _, entry := range entries {
		// Check for DPU-specific attributes
		boardIDPath := filepath.Join(dpuPath, entry.Name(), "board_id")
		//nolint:gosec // G304: Path constructed from trusted system paths, not user input
		data, err := os.ReadFile(boardIDPath)
		if err == nil {
			boardID := strings.TrimSpace(string(data))
			// Check if this is a DPU (BlueField has specific board IDs)
			if strings.Contains(boardID, "MT4") || strings.Contains(boardID, "BF") {
				dpu := DPUInfo{
					Index:       len(dpus),
					Name:        entry.Name(),
					DevicePath:  filepath.Join(dpuPath, entry.Name()),
					RDMASupport: true,
				}
				dpus = append(dpus, dpu)
			}
		}
	}

	return dpus
}

// detectRDMADevices detects RDMA-capable network devices.
func (d *Detector) detectRDMADevices() []RDMAInfo {
	// Check sysfs for RDMA devices
	rdmaPath := "/sys/class/infiniband"

	entries, err := os.ReadDir(rdmaPath)
	if err != nil {
		log.Debug().Msg("No RDMA devices found in sysfs")
		return nil
	}

	devices := make([]RDMAInfo, 0, len(entries))

	for _, entry := range entries {
		devicePath := filepath.Join(rdmaPath, entry.Name())
		device := RDMAInfo{
			Name:       entry.Name(),
			DevicePath: devicePath,
		}

		// Read device attributes
		device.NodeGUID = d.readSysfsFile(filepath.Join(devicePath, "node_guid"))
		device.SysImageGUID = d.readSysfsFile(filepath.Join(devicePath, "sys_image_guid"))
		device.BoardID = d.readSysfsFile(filepath.Join(devicePath, "board_id"))
		device.FirmwareVer = d.readSysfsFile(filepath.Join(devicePath, "fw_ver"))

		// Read node type
		nodeType := d.readSysfsFile(filepath.Join(devicePath, "node_type"))
		device.NodeType = d.parseNodeType(nodeType)

		// Count physical ports
		portsPath := filepath.Join(devicePath, "ports")
		portEntries, err := os.ReadDir(portsPath)
		if err == nil {
			device.PhysPortCount = len(portEntries)

			// Get link info from first port
			if len(portEntries) > 0 {
				port1Path := filepath.Join(portsPath, portEntries[0].Name())
				device.LinkLayer = d.readSysfsFile(filepath.Join(port1Path, "link_layer"))
				device.State = d.readSysfsFile(filepath.Join(port1Path, "state"))
				device.Speed = d.parseSpeed(d.readSysfsFile(filepath.Join(port1Path, "rate")))
			}
		}

		devices = append(devices, device)
	}

	return devices
}

// readSysfsFile reads a sysfs file and returns its content.
func (d *Detector) readSysfsFile(path string) string {
	//nolint:gosec // G304: path is from trusted sysfs locations
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

// parseNodeType converts node type number to string.
func (d *Detector) parseNodeType(nodeType string) string {
	switch strings.TrimSpace(nodeType) {
	case "1":
		return "CA" // Channel Adapter
	case "2":
		return "Switch"
	case "3":
		return "Router"
	default:
		return "Unknown"
	}
}

// parseSpeed parses speed string to Gb/s.
func (d *Detector) parseSpeed(rate string) uint64 {
	// Rate is usually in format "100 Gb/sec (4X EDR)"
	parts := strings.Fields(rate)
	if len(parts) >= 1 {
		speed, _ := strconv.ParseUint(parts[0], 10, 64)
		return speed
	}

	return 0
}

// GetRecommendedConfig returns recommended configuration based on hardware.
func (d *Detector) GetRecommendedConfig() map[string]interface{} {
	caps := d.GetCapabilities()
	config := make(map[string]interface{})

	if caps.GPUAvailable {
		config["gpudirect"] = d.buildGPUConfig(&caps)
	}

	if caps.DPUAvailable {
		config["dpu"] = d.buildDPUConfig(&caps)
	}

	if caps.RDMAAvailable {
		config["rdma"] = d.buildRDMAConfig(&caps)
	}

	return config
}

// buildGPUConfig builds GPU configuration based on capabilities.
func (d *Detector) buildGPUConfig(caps *HardwareCapabilities) map[string]interface{} {
	gpuConfig := map[string]interface{}{
		"enabled":          true,
		"buffer_pool_size": 1024 * 1024 * 1024, // 1GB default
		"enable_async":     true,
		"enable_p2p":       false,
	}

	// Enable P2P if multiple GPUs with P2P support
	p2pCount := 0
	for _, gpu := range caps.GPUs {
		if gpu.P2PSupported {
			p2pCount++
		}
	}
	if p2pCount >= 2 {
		gpuConfig["enable_p2p"] = true
	}

	// Check for GDS support
	for _, gpu := range caps.GPUs {
		if gpu.GDSSupported {
			gpuConfig["enable_gds"] = true
			break
		}
	}

	return gpuConfig
}

// buildDPUConfig builds DPU configuration based on capabilities.
func (d *Detector) buildDPUConfig(caps *HardwareCapabilities) map[string]interface{} {
	dpuConfig := map[string]interface{}{
		"enabled":            true,
		"enable_crypto":      false,
		"enable_compression": false,
		"enable_rdma":        false,
	}

	for _, dpu := range caps.DPUs {
		if dpu.CryptoSupport {
			dpuConfig["enable_crypto"] = true
		}
		if dpu.CompressSupport {
			dpuConfig["enable_compression"] = true
		}
		if dpu.RDMASupport {
			dpuConfig["enable_rdma"] = true
		}
	}

	return dpuConfig
}

// buildRDMAConfig builds RDMA configuration based on capabilities.
func (d *Detector) buildRDMAConfig(caps *HardwareCapabilities) map[string]interface{} {
	rdmaConfig := map[string]interface{}{
		"enabled":          true,
		"port":             9100,
		"enable_zero_copy": true,
		"fallback_to_tcp":  true,
	}

	// Find best device
	var (
		bestDevice *RDMAInfo
		maxSpeed   uint64
	)

	for i := range caps.RDMADevices {
		dev := &caps.RDMADevices[i]
		if dev.State == "ACTIVE" && dev.Speed > maxSpeed {
			bestDevice = dev
			maxSpeed = dev.Speed
		}
	}

	if bestDevice != nil {
		rdmaConfig["device_name"] = bestDevice.Name
	}

	return rdmaConfig
}
