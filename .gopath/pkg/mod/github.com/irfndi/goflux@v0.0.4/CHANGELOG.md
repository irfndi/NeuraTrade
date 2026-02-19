# GoFlux Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Forked from [techan](https://github.com/sdcoffey/techan) by sdcoffey
- GitHub Actions CI/CD pipelines
- golangci-lint configuration
- Comprehensive documentation updates
- CONTRIBUTING.md guidelines
- Issue and PR templates

### Changed
- Updated Go version baseline and dependencies
- Updated all dependencies to latest versions
- Renamed package from `techan` to `goflux`
- Modernized Makefile

### Removed
- Travis CI configuration (replaced with GitHub Actions)

---

## Pre-GoFlux History (techan)

> Below are the releases from the original [techan](https://github.com/sdcoffey/techan) project before it became GoFlux.

### 0.12.1 – 0.1.0
- **0.12.1**: Fixed EMA window calculation (thanks @danhenke & @joelnordell)  
- **0.12.0**: Added MaximumValue, MinimumValue and MaximumDrawdown indicators  
- **0.11.0**: Added BollingerUpperBand & BollingerLowerBand indicators (thanks @shkim)  
- **0.10.0**: Added TimePeriod#In and TimePeriod#UTC helpers for time-zone handling  
- **0.9.0**: Added Aroon indicator; deprecated Parse in favour of ParseTimePeriod  
- **0.8.0**: Added MMA, Gain & Loss indicators; fixed RSI bug (#13)  
- **0.7.1**: Fixed trend-line low-index OOB error  
- **0.7.0**: Added Trendline indicator; updated big to v0.4.1  
- **0.6.1**: Fixed TotalProfitAnalysis short-position bug (#10)  
- **0.6.0**: **BREAKING** – StdDev & Variance indicators now follow NewXIndicator pattern; migrated to Go modules  
- **0.5.0**: Added StandardDeviation & Variance indicators  
- **0.4.0**: Added DerivativeIndicator  
- **0.3.0**: Renamed talib4g → techan  
- **0.2.0**: Removed NewOrder constructors; added tests & godoc  
- **0.1.1**: Documentation updates  
- **0.1.0**: Initial talib4g release with basic indicators, time-series, strategies, entry & exit rules
