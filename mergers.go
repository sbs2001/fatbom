package main

import (
	"fmt"
	"path"
	"strings"
	"time"

	spdx22JSON "sigs.k8s.io/bom/pkg/spdx/json/v2.2.2"
)

type ExternalRefKey struct {
	Type    string
	Locator string
}

type PackageWithBomTool struct {
	BomTool string
	Package SpdxPackage
}

func mergedCreationInfo(bomByTools map[string]SpdxDocument) spdx22JSON.CreationInfo {
	ret := spdx22JSON.CreationInfo{}
	for _, bom := range bomByTools {
		ret.Creators = append(ret.Creators, bom.CreationInfo.Creators...)
		if ret.LicenseListVersion < bom.CreationInfo.LicenseListVersion {
			ret.LicenseListVersion = bom.CreationInfo.LicenseListVersion
		}
	}
	ret.Created = time.Now().Format(time.RFC3339)
	return ret
}

func mergedFiles(bomByTools map[string]SpdxDocument) []spdx22JSON.File {
	normalizeFilePaths(bomByTools)
	files := make([]spdx22JSON.File, 0)
	fileByName := make(map[string]spdx22JSON.File)

	for tool, doc := range bomByTools {
		for _, file := range doc.Files {
			if _, ok := fileByName[file.Name]; !ok || tool == K8Bom.String() {
				// we prefer K8Bom because it detects license in file
				fileByName[file.Name] = file
			}
		}
	}
	for _, file := range fileByName {
		fileSHA1 := ""
		for _, hash := range file.Checksums {
			if hash.Algorithm == "SHA1" {
				fileSHA1 = hash.Value
				break
			}
		}
		file.ID = fmt.Sprintf("SPDXRef-File-%s-%s", file.Name, fileSHA1)
		files = append(files, file)
	}
	return files
}

func normalizeFilePaths(bomByTools map[string]SpdxDocument) {
	for t, doc := range bomByTools {
		for i, _ := range doc.Files {
			bomByTools[t].Files[i].Name = path.Clean(bomByTools[t].Files[i].Name)
		}
	}
}

// FIXME: Once, Spdx bom generator provides purls, the key of this index should be purls instead of name+version
func createPackageIndex(bomByTools map[string]SpdxDocument) map[string][]PackageWithBomTool {
	pbtByNameVersion := make(map[string][]PackageWithBomTool)
	for tool, bom := range bomByTools {
		for _, p := range bom.Packages {
			key := fmt.Sprintf("%s-%s", p.Name, p.Version)
			pbt := PackageWithBomTool{Package: p, BomTool: tool}
			pbtByNameVersion[key] = append(pbtByNameVersion[key], pbt)
		}
	}
	return pbtByNameVersion
}

func mergedPackages(bomByTools map[string]SpdxDocument) []SpdxPackage {
	idx := createPackageIndex(bomByTools)
	ret := make([]SpdxPackage, 0)
	for _, pbts := range idx {
		if len(pbts) == 1 && len(pbts[0].Package.HasFiles) != 0 {
			continue
		}
		p := pbts[0].Package
		extRefSet := make(map[ExternalRefKey]SpdxPackageExternalRef)
		for _, pbt := range pbts {
			for _, ref := range pbt.Package.ExternalRefs {
				key := ExternalRefKey{Type: ref.Type, Locator: ref.Locator}
				if !strings.Contains(extRefSet[key].Comment, pbt.BomTool) {
					ref.Comment = extRefSet[key].Comment + fmt.Sprintf("Found by %s Tool. ", pbt.BomTool)
				}
				extRefSet[key] = ref
			}
		}
		p.ExternalRefs = []SpdxPackageExternalRef{}
		for _, ref := range extRefSet {
			p.ExternalRefs = append(p.ExternalRefs, ref)
		}
		if p.Name != "" {
			ret = append(ret, p)
		}
	}
	return ret
}

func createRelationships(doc *SpdxDocument) {
	fileRelationships := make([]spdx22JSON.Relationship, len(doc.Files))
	pkgRelationships := make([]spdx22JSON.Relationship, len(doc.Packages))

	for i, file := range doc.Files {
		fileRelationships[i].Type = "CONTAINS"
		fileRelationships[i].Element = "SPDXRef-Package-scan"
		fileRelationships[i].Related = file.ID
	}

	for i, pkg := range doc.Packages {
		pkgRelationships[i].Type = "DEPENDS_ON"
		pkgRelationships[i].Element = "SPDXRef-Package-scan"
		pkgRelationships[i].Related = pkg.ID
	}
	doc.Relationships = append(doc.Relationships, fileRelationships...)
	doc.Relationships = append(doc.Relationships, pkgRelationships...)
}

func addRootPackage(bom *SpdxDocument) {
	rootPkg := SpdxPackage{}
	rootPkg.ID = "SPDXRef-Package-RootPackage"
	rootPkg.Name = "RootPackage"
	licenseSet := make(map[string]struct{})
	for _, file := range bom.Files {
		rootPkg.HasFiles = append(rootPkg.HasFiles, file.ID)
		for _, license := range file.LicenseInfoInFile {
			licenseSet[license] = struct{}{}
		}
	}
	for license := range licenseSet {
		rootPkg.LicenseInfoFromFiles = append(rootPkg.LicenseInfoFromFiles, license)
	}
	rootPkg.LicenseConcluded = spdx22JSON.NOASSERTION
	rootPkg.LicenseDeclared = spdx22JSON.NOASSERTION
	rootPkg.DownloadLocation = "NONE"
	bom.Packages = append([]SpdxPackage{rootPkg}, bom.Packages...)
}

func Merge(bomByTools map[string]SpdxDocument) SpdxDocument {
	ret := SpdxDocument{}
	ret.CreationInfo = mergedCreationInfo(bomByTools)
	ret.Packages = mergedPackages(bomByTools)
	ret.Files = mergedFiles(bomByTools)
	createRelationships(&ret)
	addRootPackage(&ret)
	ret.Name = fmt.Sprintf("SPDX-SBOM-%s", fullPathToDirToScan)
	ret.DataLicense = "CC0-1.0"
	ret.DocumentDescribes = []string{"SPDXRef-Package-scan"}
	ret.Version = "SPDX-2.2"
	ret.ID = "SPDXRef-DOCUMENT"
	return ret
}
