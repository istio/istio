FROM tomcat:jdk16-openjdk
COPY . /usr/local/tomcat/webapps/
RUN ["javac", "-cp", ".:/usr/local/tomcat/lib/servlet-api.jar", "-d", "/usr/local/tomcat/webapps/private/WEB-INF/classes/", "/usr/local/tomcat/webapps/private/HelloWorld.java"]
EXPOSE 8080
CMD ["catalina.sh", "run"]
