package main

import (
	"fmt"
	"os"

	"github.com/apporbit/infranetes/pkg/infranetes/provider/gcp"
)

func main() {
	args := os.Args

	if len(args) != 4 {
		fmt.Println("Usage:\n\ttest [1|0] dest (ip)")
		return
	}

	svc, err := gcp.GetService("/home/spotter/gcp.json", "engineering-lab", "us-central1-b", []string{"https://www.googleapis.com/auth/cloud-platform"})
	if err != nil {
		fmt.Printf("GetService failed: %v\n", err)
		return
	}

	switch args[1] {
	case "0":
		err := svc.AddRoute(args[2], args[3])
		if err != nil {
			fmt.Printf("Failed to add route: %v\n", err)
		} else {
			fmt.Printf("Succeeded to add route\n")
		}
		return
	case "1":
		err := svc.DelRoute(args[2])
		if err != nil {
			fmt.Printf("Failed to del route: %v\n", err)
		} else {
			fmt.Printf("Succeeded to del route\n")
		}
		return
	default:
		fmt.Printf("Unknown argument: %v\n", args[1])
		return
	}
}
