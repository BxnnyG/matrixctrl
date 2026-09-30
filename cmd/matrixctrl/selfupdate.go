package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/helm"
	"github.com/bxnnyg/matrixctrl/internal/selfupdate"
)

// runSelfUpdate upgrades MatrixCtrl's own release (etappe 116). Run by the update Job;
// its stdout is what the panel shows as the update log, so every line is for a person.
func runSelfUpdate(args []string) int {
	fs := flag.NewFlagSet("self-update", flag.ContinueOnError)
	release := fs.String("release", "matrixctrl", "MatrixCtrl's Helm release")
	namespace := fs.String("namespace", "matrixctrl", "its namespace")
	target := fs.String("version", "", "chart version to upgrade to")
	if err := fs.Parse(args); err != nil || *target == "" {
		log.Printf("Aufruf: matrixctrl self-update --version <x.y.z> [--release matrixctrl] [--namespace matrixctrl]")
		return 2
	}
	log.SetFlags(log.Ltime)
	log.Printf("Update von MatrixCtrl auf %s", *target)

	c, err := helm.New(*namespace)
	if err != nil {
		log.Printf("FEHLER: Helm nicht verfügbar: %v", err)
		return 1
	}
	values, err := c.ReleaseUserValues(*release)
	if err != nil {
		log.Printf("FEHLER: die Werte des laufenden Releases sind nicht lesbar: %v", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := c.UpgradeSelf(ctx, *release, *target, selfupdate.CarryValues(values), log.Printf); err != nil {
		log.Printf("FEHLER: %v", err)
		log.Printf("Die vorige Version läuft weiter — das Upgrade wurde zurückgerollt.")
		return 1
	}
	log.Printf("Fertig: MatrixCtrl %s läuft.", *target)
	return 0
}
