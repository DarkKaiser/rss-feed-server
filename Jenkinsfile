pipeline {

    agent any

    environment {
        PROJECT_NAME = "RSS Feed 서버"
    }

    stages {

        stage('준비') {
            steps {
                cleanWs()
            }
        }

        stage('소스 체크아웃') {
            steps {
                checkout([
                    $class: 'GitSCM',
                    branches: [[ name: '*/main' ]],
                    extensions: [[
                        $class: 'SubmoduleOption',
                        disableSubmodules: false,
                        parentCredentials: true,
                        recursiveSubmodules: false,
                        reference: '',
                        trackingSubmodules: true
                    ]],
                    submoduleCfg: [],
                    userRemoteConfigs: [[
                        credentialsId: 'github-darkkaiser-credentials',
                        url: 'https://github.com/DarkKaiser/rss-feed-server.git'
                    ]]
                ])
            }
        }

        stage('도커 이미지 빌드') {
            steps {
                sh "docker build -t darkkaiser/rss-feed-server ."
            }
        }

        stage('도커 컨테이너 실행') {
            steps {
                sh '''
                    docker ps -q --filter name=rss-feed-server | grep -q . && docker container stop rss-feed-server && docker container rm rss-feed-server

                    docker run -d --name rss-feed-server \
                                  -e TZ=Asia/Seoul \
                                  -v /usr/local/docker/rss-feed-server:/usr/local/app \
                                  -v /etc/letsencrypt/:/etc/letsencrypt/ \
                                  -p 443:443 \
                                  --restart="always" \
                                  darkkaiser/rss-feed-server
                '''
            }
        }

        stage('도커 이미지 정리') {
            steps {
                sh 'docker images -qf dangling=true | xargs -I{} docker rmi {}'
            }
        }

    }

    post {

        success {
            script {
                sh "curl -s -X POST https://api.telegram.org/bot${env.TELEGRAM_BOT_TOKEN}/sendMessage -d chat_id=${env.TELEGRAM_CHAT_ID} -d text='【 알림 > Jenkins > ${env.PROJECT_NAME} 】\n\n빌드 작업이 성공하였습니다.\n\n${env.BUILD_URL}'"
            }
        }

        failure {
            script {
                sh "curl -s -X POST https://api.telegram.org/bot${env.TELEGRAM_BOT_TOKEN}/sendMessage -d chat_id=${env.TELEGRAM_CHAT_ID} -d text='【 알림 > Jenkins > ${env.PROJECT_NAME} 】\n\n빌드 작업이 실패하였습니다.\n\n${env.BUILD_URL}'"
            }
        }

        always {
            cleanWs()
        }

    }

}