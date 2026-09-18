// ponytail: P5 strangler stub — version/health only until risk-first ports land
fn main() {
    let args: Vec<String> = std::env::args().collect();
    match args.get(1).map(String::as_str) {
        Some("health") => println!("nt-cli 0.1.0 status=ok runtime=rust-strangler"),
        _ => println!("nt-cli 0.1.0"),
    }
}
