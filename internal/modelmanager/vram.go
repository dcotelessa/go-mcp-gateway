package modelmanager

import "fmt"

// ErrModelTooLargeForVRAM is returned when a model's VRAM requirement
// exceeds the absolute hardware ceiling (total VRAM + system RAM headroom).
// llama.cpp can split layers across GPU VRAM and system RAM via -sm layer,
// so models larger than VRAM alone are still loadable.
var ErrModelTooLargeForVRAM = fmt.Errorf("modelmanager: model exceeds hardware capacity")

// VRAMBudget holds computed VRAM allocation for a model load.
type VRAMBudget struct {
	TotalMiB        int
	ReservedMiB     int
	AvailableMiB    int
	ModelMiB        int
	NeedsLayerSplit bool
}

// hardCeilingMiB is the absolute maximum we'll attempt to load.
// llama.cpp can offload layers to system RAM so models larger than
// VRAM are fine — this ceiling prevents obviously impossible loads
// (e.g. a 500B model on a 32GB RAM machine).
// Set conservatively: total VRAM + 48GB system RAM headroom.
const hardCeilingMultiplier = 4

// ComputeBudget checks whether a model is loadable and whether a
// GPU/CPU layer split is needed.
// NeedsLayerSplit = true when model exceeds available VRAM after reservation
// but is still within the hard ceiling (llama.cpp handles the rest via RAM).
func ComputeBudget(totalMiB, reservedMiB, modelMiB int) (VRAMBudget, error) {
	hardCeiling := totalMiB * hardCeilingMultiplier
	if modelMiB > hardCeiling {
		return VRAMBudget{}, fmt.Errorf(
			"%w: model %dMiB exceeds ceiling %dMiB",
			ErrModelTooLargeForVRAM, modelMiB, hardCeiling,
		)
	}

	available := totalMiB - reservedMiB
	needsSplit := modelMiB > available

	return VRAMBudget{
		TotalMiB:        totalMiB,
		ReservedMiB:     reservedMiB,
		AvailableMiB:    available,
		ModelMiB:        modelMiB,
		NeedsLayerSplit: needsSplit,
	}, nil
}

// LayerSplitArgs returns the extra args needed when a model requires
// GPU/CPU layer splitting due to VRAM constraints.
func LayerSplitArgs(budget VRAMBudget) []string {
	if !budget.NeedsLayerSplit {
		return nil
	}
	return []string{"-sm", "layer"}
}
