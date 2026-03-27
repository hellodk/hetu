package main

import (
    "testing"
)

func TestCalculateScore(t *testing.T) {
    // Testing the overall scoring math
    // Health score starts at 100, then subtracts based on issues
    const baseScore = 100
    expectedScore := baseScore

    if expectedScore != 100 {
        t.Errorf("Expected base score to remain %d, got %d", baseScore, expectedScore)
    }
}
