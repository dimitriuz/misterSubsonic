package viz

import "math"

// fft is a radix-2, in-place, float32 FFT of a fixed power-of-two size.
type fft struct {
	n        int
	rev      []int     // bit-reversal permutation
	cos, sin []float32 // twiddles e^(-2πik/n), k < n/2
}

func newFFT(n int) *fft {
	if n < 2 || n&(n-1) != 0 {
		panic("viz: FFT size must be a power of two")
	}
	f := &fft{n: n, rev: make([]int, n), cos: make([]float32, n/2), sin: make([]float32, n/2)}
	bits := 0
	for 1<<bits < n {
		bits++
	}
	for i := range f.rev {
		r := 0
		for b := 0; b < bits; b++ {
			if i&(1<<b) != 0 {
				r |= 1 << (bits - 1 - b)
			}
		}
		f.rev[i] = r
	}
	for k := range f.cos {
		a := -2 * math.Pi * float64(k) / float64(n)
		f.cos[k] = float32(math.Cos(a))
		f.sin[k] = float32(math.Sin(a))
	}
	return f
}

// transform replaces (re, im) with its discrete Fourier transform.
func (f *fft) transform(re, im []float32) {
	for i, j := range f.rev {
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for size := 2; size <= f.n; size <<= 1 {
		half, step := size/2, f.n/size
		for start := 0; start < f.n; start += size {
			for k := 0; k < half; k++ {
				wr, wi := f.cos[k*step], f.sin[k*step]
				a, b := start+k, start+k+half
				tr := re[b]*wr - im[b]*wi
				ti := re[b]*wi + im[b]*wr
				re[b], im[b] = re[a]-tr, im[a]-ti
				re[a] += tr
				im[a] += ti
			}
		}
	}
}
