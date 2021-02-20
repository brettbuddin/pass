upstream "accounts" {
    destination = "${destination}" 
    owner = "Identity <team-identity@company.com>"

    route {
        methods = ["GET"]
        path = "/accounts"
    }
}
