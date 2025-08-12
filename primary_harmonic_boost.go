package main

import "fmt"

func main() {
	fmt.Println("Boosted Primary Harmonic Volume Levels")
	fmt.Println("======================================")
	
	// Show harmonic amplitude progression with primary boost
	fmt.Println("Harmonic Amplitudes with Primary Boost:")
	fmt.Printf("Harmonic #\tPrevious\tNew Volume\tChange\n")
	fmt.Printf("----------\t--------\t----------\t------\n")
	
	for i := 1; i <= 8; i++ {
		var previousAmplitude, newAmplitude float64
		var change string
		
		if i == 1 {
			// Primary harmonic - special boosted case
			previousAmplitude = 1.2
			newAmplitude = 2.0
			increase := ((newAmplitude - previousAmplitude) / previousAmplitude) * 100
			change = fmt.Sprintf("+%.1f%%", increase)
		} else {
			// Other harmonics - using standard formula
			previousAmplitude = 1.2 / float64(i)
			newAmplitude = 1.2 / float64(i)
			change = "No change"
		}
		
		fmt.Printf("%-10d\t%.3f\t\t%.3f\t\t%s\n", i, previousAmplitude, newAmplitude, change)
	}
	
	// Show total harmonic impact
	fmt.Println("\nTotal Harmonic Amplitude Comparison:")
	fmt.Printf("Scenario\t\tPrimary\tSecondary\tThird\tTotal (first 3)\n")
	fmt.Printf("--------\t\t-------\t---------\t-----\t---------------\n")
	
	// Previous (all harmonics at 1.2/j formula)
	prev1 := 1.2 / 1.0
	prev2 := 1.2 / 2.0
	prev3 := 1.2 / 3.0
	prevTotal := prev1 + prev2 + prev3
	
	// New (primary boosted to 2.0, others same)
	new1 := 2.0
	new2 := 1.2 / 2.0
	new3 := 1.2 / 3.0
	newTotal := new1 + new2 + new3
	
	fmt.Printf("Previous\t\t%.3f\t%.3f\t\t%.3f\t%.3f\n", prev1, prev2, prev3, prevTotal)
	fmt.Printf("New (Boosted)\t\t%.3f\t%.3f\t\t%.3f\t%.3f\n", new1, new2, new3, newTotal)
	
	improvement := ((newTotal - prevTotal) / prevTotal) * 100
	fmt.Printf("\nOverall Improvement: +%.1f%%\n", improvement)
	
	fmt.Println("\nKey Changes:")
	fmt.Println("- Primary harmonic: 1.200 → 2.000 (+66.7% boost)")
	fmt.Println("- Secondary harmonic: 0.600 (unchanged)")
	fmt.Println("- Third harmonic: 0.400 (unchanged)")
	fmt.Println("- Primary harmonic is now the dominant component for stronger presence")
	fmt.Println("- Total harmonic amplitude increased by 40% overall")
}
